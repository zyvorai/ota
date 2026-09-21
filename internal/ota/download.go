// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Downloader struct {
	Config Config
	Client *http.Client
}

func (d Downloader) checkURL(u *url.URL) error {
	if u.User != nil || u.Fragment != "" || u.Host == "" {
		return errors.New("invalid artifact URL")
	}
	secure := u.Scheme == "https"
	localDemo := d.Config.Backend == "simulator" && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if !secure && !localDemo {
		return errors.New("artifact requires HTTPS (simulator permits loopback HTTP)")
	}
	for _, h := range d.Config.DownloadHosts {
		if strings.EqualFold(h, u.Host) {
			return nil
		}
	}
	return errors.New("artifact host not allowlisted")
}
func CheckArtifact(path string, a Artifact) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() != a.Size {
		return errors.New("artifact size mismatch")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("artifact digest mismatch")
	}
	return nil
}
func (d Downloader) Fetch(ctx context.Context, a Artifact) (string, error) {
	path, _, _, err := d.fetch(ctx, a)
	return path, err
}

func (d Downloader) fetch(ctx context.Context, a Artifact) (string, int64, bool, error) {
	u, err := url.Parse(a.URL)
	if err != nil {
		return "", 0, false, err
	}
	if u.Scheme == "file" {
		return d.fetchFile(a, u)
	}
	if err = d.checkURL(u); err != nil {
		return "", 0, false, err
	}
	if !digestPattern.MatchString(a.SHA256) || a.Size <= 0 || a.Size > d.Config.MaxArtifactBytes {
		return "", 0, false, errors.New("invalid artifact")
	}
	dir := filepath.Join(d.Config.StateDir, "cache")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", 0, false, err
	}
	final := filepath.Join(dir, a.SHA256+".raucb")
	part := final + ".part"
	if CheckArtifact(final, a) == nil {
		return final, a.Size, false, nil
	}
	if err = os.Remove(final); err != nil && !os.IsNotExist(err) {
		return "", 0, false, err
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, false, err
	}
	if !st.Mode().IsRegular() {
		return "", 0, false, errors.New("cache entry is not a regular file")
	}
	offset := st.Size()
	if offset > a.Size {
		if err = f.Truncate(0); err != nil {
			return "", 0, false, err
		}
		offset = 0
	}
	if offset == a.Size {
		if CheckArtifact(part, a) == nil {
			if err = finishDownload(f, part, final); err != nil {
				return "", 0, false, err
			}
			return final, a.Size, true, nil
		}
		if err = f.Truncate(0); err != nil {
			return "", 0, false, err
		}
		offset = 0
	}
	var fs syscall.Statfs_t
	if err = syscall.Statfs(dir, &fs); err != nil {
		return "", 0, false, err
	}
	if fs.Bavail*uint64(fs.Bsize) < uint64(a.Size-offset)+d.Config.ReserveBytes {
		return "", 0, false, errors.New("insufficient cache space")
	}
	client := http.Client{Timeout: 30 * time.Minute}
	if d.Client != nil {
		client = *d.Client
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return d.checkURL(req.URL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", 0, false, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resumed := offset > 0
	if resumed {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, resumed, errors.New("artifact request failed")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err = f.Truncate(0); err != nil {
			return "", 0, false, err
		}
		offset = 0
		resumed = false
	case http.StatusPartialContent:
		expected := fmt.Sprintf("bytes %d-%d/%d", offset, a.Size-1, a.Size)
		if resp.Header.Get("Content-Range") != expected {
			return "", 0, true, errors.New("invalid Content-Range")
		}
	default:
		return "", 0, resumed, fmt.Errorf("artifact HTTP status %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return "", 0, resumed, errors.New("unexpected content encoding")
	}
	if resp.ContentLength >= 0 && resp.ContentLength != a.Size-offset {
		return "", 0, resumed, errors.New("artifact Content-Length mismatch")
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return "", 0, resumed, err
	}
	body := io.Reader(resp.Body)
	if d.Config.BandwidthBytesPerSec > 0 {
		body = &bpsReader{r: resp.Body, bps: d.Config.BandwidthBytesPerSec}
	}
	n, copyErr := io.Copy(f, io.LimitReader(body, a.Size-offset+1))
	syncErr := f.Sync()
	if n > a.Size-offset {
		_ = f.Truncate(0)
		return "", n, resumed, errors.New("artifact exceeds signed size")
	}
	if copyErr != nil {
		return "", n, resumed, errors.New("artifact transfer interrupted; partial retained")
	}
	if syncErr != nil {
		return "", n, resumed, syncErr
	}
	if n != a.Size-offset {
		return "", n, resumed, errors.New("artifact transfer truncated; partial retained")
	}
	if err = CheckArtifact(part, a); err != nil {
		_ = f.Truncate(0)
		return "", n, resumed, err
	}
	if err = finishDownload(f, part, final); err != nil {
		return "", n, resumed, err
	}
	return final, n, resumed, nil
}

func (d Downloader) fetchFile(a Artifact, u *url.URL) (string, int64, bool, error) {
	if d.Config.LocalMediaDir == "" {
		return "", 0, false, errors.New("file artifacts require local_media_dir")
	}
	path := filepath.Clean(u.Path)
	rel, err := filepath.Rel(d.Config.LocalMediaDir, path)
	if err != nil || !filepath.IsAbs(path) || strings.HasPrefix(rel, "..") {
		return "", 0, false, errors.New("artifact path outside local media")
	}
	if err := CheckArtifact(path, a); err != nil {
		return "", 0, false, err
	}
	dir := filepath.Join(d.Config.StateDir, "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", 0, false, err
	}
	final := filepath.Join(dir, a.SHA256+".raucb")
	if CheckArtifact(final, a) == nil {
		return final, a.Size, false, nil
	}
	if err := copyFile(path, final); err != nil {
		return "", 0, false, err
	}
	return final, a.Size, false, nil
}

type bpsReader struct {
	r    io.Reader
	bps  int64
	sent int64
	t0   time.Time
}

func (b *bpsReader) Read(p []byte) (int, error) {
	if b.t0.IsZero() {
		b.t0 = time.Now()
	}
	if b.bps > 0 && int64(len(p)) > b.bps {
		p = p[:b.bps]
	}
	n, err := b.r.Read(p)
	b.sent += int64(n)
	if b.bps > 0 && b.sent > 0 {
		need := time.Duration(float64(b.sent) / float64(b.bps) * float64(time.Second))
		if wait := need - time.Since(b.t0); wait > 0 {
			time.Sleep(wait)
		}
	}
	return n, err
}
func finishDownload(f *os.File, part, final string) error {
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(part, final); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(final))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
