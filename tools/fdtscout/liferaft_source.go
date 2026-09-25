package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"
	"github.com/jlaffaye/ftp"
)

// This file is the actual read-only enforcement boundary. Both underlying libraries expose full
// read-write clients -- go-smb2's *Share has Create/Write/Remove/RemoveAll/Rename, and
// *ftp.ServerConn has Stor/StorFrom/Append/Delete/Rename/MakeDir/RemoveDir* -- verified directly
// against each package's real API (`go doc`), not assumed. Nothing in this codebase outside this
// file ever holds a reference to either of those types: the sync engine only ever sees the narrow
// sourceReader interface below, which has no write method to call even by mistake. That's the
// actual guarantee LifeRaft makes about never writing back to a source -- a code-level chokepoint,
// the same discipline resolveSafePath already applies to the USB file browser.

const sourceConnectTimeout = 15 * time.Second

// idleConnTimeout bounds how long a single Read or Write may go without making progress before the
// whole run aborts with a clear error, instead of hanging forever. Real gap found live: sourceConnectTimeout
// only bounds the initial TCP handshake -- neither go-smb2 nor jlaffaye/ftp set any deadline on the
// connection themselves, so a source that accepts the connection but then stalls (a firewall
// silently dropping packets rather than refusing, a server that hangs mid-negotiate) blocked the
// run forever with no error ever recorded, and since the run lock is a blocking global queue, a
// second "Run now" click just queued silently behind the same permanent hang. This bounds silence,
// not total duration -- a legitimate large transfer that keeps making some progress is unaffected.
const idleConnTimeout = 2 * time.Minute

// deadlineConn resets an idle deadline on the underlying connection before every Read/Write.
// Wrapping at the connection layer means every protocol operation (session setup, listing, file
// reads) gets this for free, the same "wrap once at the boundary" pattern already used for the
// read-only enforcement in this file.
type deadlineConn struct {
	net.Conn
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetDeadline(time.Now().Add(idleConnTimeout))
	return c.Conn.Read(b)
}

func (c *deadlineConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetDeadline(time.Now().Add(idleConnTimeout))
	return c.Conn.Write(b)
}

// sourceEntry is one file found while listing a source -- directories are never returned
// themselves, only implied by the paths of the files inside them, since LifeRaft only ever mirrors
// files, never empty directory structure.
type sourceEntry struct {
	Path    string // forward-slash relative path from the job's configured root, e.g. "Documents/report.docx"
	Size    int64
	ModTime time.Time
}

// sourceReader is the ONLY thing the sync engine (liferaft_sync.go) is ever given -- see the file
// comment above for why that matters.
type sourceReader interface {
	// List returns the immediate children of dir ("" = the job's own root), both files and
	// subdirectories, so the sync engine can recurse itself the same way for either protocol.
	List(dir string) (files []sourceEntry, subdirs []string, err error)
	Open(relPath string) (io.ReadCloser, error)
	Close() error
}

func connectLifeRaftSource(job LifeRaftJob) (sourceReader, error) {
	username, domain, password, err := DecryptedLifeRaftCredential(job.CredentialID)
	if err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}
	switch job.Protocol {
	case ProtocolSMB:
		return connectSMBSource(job, username, domain, password)
	case ProtocolFTP:
		return connectFTPSource(job, username, password, false)
	case ProtocolFTPS:
		return connectFTPSource(job, username, password, true)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", job.Protocol)
	}
}

// --- SMB -----------------------------------------------------------------------------------

type smbSource struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share // never exposed outside this file
	root    string      // job.Path within the share -- see the real bug this closes, below
}

// connectSMBSession opens a raw SMB session -- NOT yet mounted to any share -- shared by both
// connectSMBSource (which immediately mounts the job's configured share) and the browse functions
// below (liferaft_smb_browse.go), which list shares or an arbitrary path the user is auditioning
// before ever saving a job.
func connectSMBSession(host string, port int, username, domain, password string) (net.Conn, *smb2.Session, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	rawConn, err := net.DialTimeout("tcp", addr, sourceConnectTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	conn := &deadlineConn{rawConn}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: username, Password: password, Domain: domain}}
	session, err := d.Dial(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("SMB session setup failed: %w", err)
	}
	return conn, session, nil
}

func connectSMBSource(job LifeRaftJob, username, domain, password string) (sourceReader, error) {
	conn, session, err := connectSMBSession(job.Host, job.Port, username, domain, password)
	if err != nil {
		return nil, err
	}
	share, err := session.Mount(job.Share)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, fmt.Errorf("mounting share %q failed: %w", job.Share, err)
	}
	root := job.Path
	if root == "" {
		root = "/"
	}
	return &smbSource{conn: conn, session: session, share: share, root: root}, nil
}

// Real bug found live: List/Open used to operate straight off the mounted share's own root,
// completely ignoring job.Path -- every SMB job backed up the ENTIRE share, not the configured
// subfolder (e.g. "DEV Backup" configured for /#Shares/UserFolders/jason.anderson/#Dev would have
// backed up the whole "Data" share once it could connect at all). FTP already joined against its
// own root correctly; SMB never got the same treatment. Fixed to match.
func (s *smbSource) List(dir string) ([]sourceEntry, []string, error) {
	full := smbJoin(path.Join(s.root, dir))
	entries, err := s.share.ReadDir(full)
	if err != nil {
		return nil, nil, err
	}
	var files []sourceEntry
	var subdirs []string
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		if e.IsDir() {
			subdirs = append(subdirs, rel)
			continue
		}
		files = append(files, sourceEntry{Path: rel, Size: e.Size(), ModTime: e.ModTime()})
	}
	return files, subdirs, nil
}

func (s *smbSource) Open(relPath string) (io.ReadCloser, error) {
	return s.share.Open(smbJoin(path.Join(s.root, relPath)))
}

func (s *smbSource) Close() error {
	s.share.Umount()
	s.session.Logoff()
	return s.conn.Close()
}

// smbJoin converts a forward-slash relative path into the backslash form go-smb2 expects. Also
// strips a leading separator: job.Path/s.root always starts with "/" (defaulted, never blank), so
// path.Join(s.root, dir) always produces a leading "/" -- go-smb2's own validatePath explicitly
// rejects a leading '\' on Open/ReadDir ("leading '\\' is not allowed in this operation", checked
// directly against the vendored source, not assumed), so passing that straight through would have
// made every SMB job fail outright rather than just ignoring the configured subpath.
func smbJoin(relPath string) string {
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		return ""
	}
	return strings.ReplaceAll(relPath, "/", string(smb2.PathSeparator))
}

// --- FTP / FTPS ------------------------------------------------------------------------------

type ftpSource struct {
	conn *ftp.ServerConn
	root string // job.Path, so List/Open can be called with paths relative to the job's own root
}

func connectFTPSource(job LifeRaftJob, username, password string, useTLS bool) (sourceReader, error) {
	addr := net.JoinHostPort(job.Host, strconv.Itoa(job.Port))
	rawConn, err := net.DialTimeout("tcp", addr, sourceConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	opts := []ftp.DialOption{ftp.DialWithNetConn(&deadlineConn{rawConn})}
	if useTLS {
		// InsecureSkipVerify is deliberate here, not an oversight: a home/office NAS's FTPS
		// certificate is very often self-signed, and LifeRaft has no existing mechanism for the
		// user to pin a source's cert the way CloudKeyWizard/FDT.Scout's own TLS listener is
		// pinned by the browser. Read-only credentials over an encrypted-but-unverified channel is
		// still materially better than plain FTP; full cert pinning for sources is a real gap
		// worth closing in a later pass, not this one.
		opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{InsecureSkipVerify: true}))
	}
	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("FTP session setup with %s failed: %w", addr, err)
	}
	if err := conn.Login(username, password); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("login failed: %w", err)
	}
	root := job.Path
	if root == "" {
		root = "/"
	}
	return &ftpSource{conn: conn, root: root}, nil
}

func (s *ftpSource) List(dir string) ([]sourceEntry, []string, error) {
	full := path.Join(s.root, dir)
	entries, err := s.conn.List(full)
	if err != nil {
		return nil, nil, err
	}
	var files []sourceEntry
	var subdirs []string
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		rel := path.Join(dir, e.Name)
		switch e.Type {
		case ftp.EntryTypeFolder:
			subdirs = append(subdirs, rel)
		case ftp.EntryTypeFile:
			files = append(files, sourceEntry{Path: rel, Size: int64(e.Size), ModTime: e.Time})
		// EntryTypeLink and anything else is deliberately skipped -- following a symlink on an FTP
		// server this app doesn't control risks walking outside the intended tree entirely.
		default:
		}
	}
	return files, subdirs, nil
}

func (s *ftpSource) Open(relPath string) (io.ReadCloser, error) {
	full := path.Join(s.root, relPath)
	resp, err := s.conn.Retr(full)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *ftpSource) Close() error {
	return s.conn.Quit()
}
