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
	u, err := url.Parse(a.URL)
	if err != nil {
		return "", err
	}
	if err = d.checkURL(u); err != nil {
		return "", err
	}
	if !digestPattern.MatchString(a.SHA256) || a.Size <= 0 || a.Size > d.Config.MaxArtifactBytes {
		return "", errors.New("invalid artifact")
	}
	dir := filepath.Join(d.Config.StateDir, "cache")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	final := filepath.Join(dir, a.SHA256+".raucb")
	part := final + ".part"
	if CheckArtifact(final, a) == nil {
		return final, nil
	}
	if err = os.Remove(final); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("cache entry is not a regular file")
	}
	offset := st.Size()
	if offset > a.Size {
		if err = f.Truncate(0); err != nil {
			return "", err
		}
		offset = 0
	}
	if offset == a.Size {
		if CheckArtifact(part, a) == nil {
			return final, finishDownload(f, part, final)
		}
		if err = f.Truncate(0); err != nil {
			return "", err
		}
		offset = 0
	}
	var fs syscall.Statfs_t
	if err = syscall.Statfs(dir, &fs); err != nil {
		return "", err
	}
	if fs.Bavail*uint64(fs.Bsize) < uint64(a.Size-offset)+d.Config.ReserveBytes {
		return "", errors.New("insufficient cache space")
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
		return "", err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("artifact request failed")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err = f.Truncate(0); err != nil {
			return "", err
		}
		offset = 0
	case http.StatusPartialContent:
		expected := fmt.Sprintf("bytes %d-%d/%d", offset, a.Size-1, a.Size)
		if resp.Header.Get("Content-Range") != expected {
			return "", errors.New("invalid Content-Range")
		}
	default:
		return "", fmt.Errorf("artifact HTTP status %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return "", errors.New("unexpected content encoding")
	}
	if resp.ContentLength >= 0 && resp.ContentLength != a.Size-offset {
		return "", errors.New("artifact Content-Length mismatch")
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, a.Size-offset+1))
	syncErr := f.Sync()
	if n > a.Size-offset {
		_ = f.Truncate(0)
		return "", errors.New("artifact exceeds signed size")
	}
	if copyErr != nil {
		return "", errors.New("artifact transfer interrupted; partial retained")
	}
	if syncErr != nil {
		return "", syncErr
	}
	if n != a.Size-offset {
		return "", errors.New("artifact transfer truncated; partial retained")
	}
	if err = CheckArtifact(part, a); err != nil {
		_ = f.Truncate(0)
		return "", err
	}
	return final, finishDownload(f, part, final)
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
