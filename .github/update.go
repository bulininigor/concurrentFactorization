package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// no external libraries were used specifically

const (
	staffDirectory      = ".github"
	defaultMetadata     = "metadata.json"
	defaultLocalVersion = "version.txt"
)

type fileMap struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type metadata struct {
	Repo        string    `json:"repo"`
	Ref         string    `json:"ref,omitempty"`
	VersionPath string    `json:"version_path"`
	Files       []fileMap `json:"files"`
}

type repoSpec struct {
	Owner string
	Repo  string
	Ref   string
}

var dialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 180 * time.Second,
}

var transport = &http.Transport{
	DialContext:           dialer.DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxConnsPerHost:       100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 2 * time.Second,
	MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
}

var client = http.Client{
	Transport: transport,
	Timeout:   30 * time.Second,
}

func main() {
	metadataPath := filepath.Join(staffDirectory, defaultMetadata)
	localVersionPath := filepath.Join(staffDirectory, defaultLocalVersion)
	force := flag.Bool("force", false, "for update")
	flag.Parse()

	md, spec, err := parseMetadata(metadataPath)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("> Source: %s/%s", spec.Owner, spec.Repo)

	if spec.Ref != "" {
		fmt.Printf(" @ %s", spec.Ref)
	}

	fmt.Println()

	token := cmp.Or(os.Getenv("IGOROUTINE_COURSES_GITHUB_TOKEN"), os.Getenv("GITHUB_TOKEN"))

	if token != "" {
		fmt.Println("✓ The github token has been processed")
	}

	remoteVerBytes, err := fetchGitHubContentRaw(spec, md.VersionPath, token)

	if err != nil {
		fmt.Fprintf(os.Stderr, "can not fetch tests repository version (%s): %v\n", md.VersionPath, err)
		os.Exit(1)
	}
	remoteVer := strings.TrimSpace(string(remoteVerBytes))

	localVer := ""
	b, err := os.ReadFile(localVersionPath)

	if err == nil {
		localVer = strings.TrimSpace(string(b))
	} else if os.IsNotExist(err) {
		f, createErr := os.Create(localVersionPath)

		if createErr != nil {
			fmt.Fprintf(os.Stderr, "can not create version file (%s): %v\n", md.VersionPath, err)
			os.Exit(-1)
		}

		f.Close()
		localVer = ""
	} else {
		fmt.Fprintf(os.Stderr, "can not read version file (%s): %v\n", md.VersionPath, err)
		os.Exit(-1)
	}

	if !(*force) && localVer == remoteVer {
		fmt.Printf("✓ The version is up to date (%s)\n", remoteVer)
		return
	}

	if *force {
		fmt.Printf("↺ Force update, ignore local version %q, remote version %q)\n", localVer, remoteVer)
	} else {
		fmt.Printf("↺ Download tests: local version %q → remote version %q\n", localVer, remoteVer)
	}

	root, err := os.Getwd()

	if err != nil {
		fmt.Fprintf(os.Stderr, "can not get working directory: %v\n", err)
		os.Exit(1)
	}

	for _, fm := range md.Files {
		if strings.TrimSpace(fm.From) == "" || strings.TrimSpace(fm.To) == "" {
			fmt.Fprintln(os.Stderr, "from/to missed in files")
			os.Exit(1)
		}

		fmt.Printf("→ Fetch: %s\n", fm.From)
		content, err := fetchGitHubContentRaw(spec, fm.From, token)

		if err != nil {
			fmt.Fprintf(os.Stderr, "  download error %s: %v\n", fm.From, err)
			os.Exit(1)
		}

		dst, err := safeJoin(root, fm.To)

		if err != nil {
			fmt.Fprintf(os.Stderr, "  invalid dst path %q: %v\n", fm.To, err)
			os.Exit(1)
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "  mkdir %s: %v\n", filepath.Dir(dst), err)
			os.Exit(1)
		}

		if err := os.WriteFile(dst, content, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  write %s: %v\n", dst, err)
			os.Exit(1)
		}

		fmt.Printf("  ✓ Saved: %s\n", relOrSame(root, dst))
	}

	if err := os.MkdirAll(filepath.Dir(localVersionPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(localVersionPath), err)
		os.Exit(1)
	}

	if err := os.WriteFile(localVersionPath, []byte(remoteVer+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", localVersionPath, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tests updated: %s → %s\n", localVer, remoteVer)
	fmt.Println("Success!")
}

func parseMetadata(path string) (metadata, repoSpec, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return metadata{}, repoSpec{}, err
	}
	var md metadata
	if err = json.Unmarshal(data, &md); err != nil {
		return metadata{}, repoSpec{}, fmt.Errorf("json.Unmarshal: %w", err)
	}

	if md.Repo == "" {
		return metadata{}, repoSpec{}, errors.New("missed repos in metadata.json")
	}

	if md.VersionPath == "" {
		return metadata{}, repoSpec{}, errors.New("missed version_path in metadata.json")
	}

	if len(md.Files) == 0 {
		return metadata{}, repoSpec{}, errors.New("missed files in metadata.json")
	}

	owner, repo := splitOwnerRepo(md.Repo)

	if owner == "" || repo == "" {
		return metadata{}, repoSpec{}, fmt.Errorf("find empty repository path: %s", md.Repo)
	}

	return md, repoSpec{Owner: owner, Repo: repo, Ref: md.Ref}, nil
}

func splitOwnerRepo(s string) (string, string) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "git@github.com:")
	s = strings.TrimPrefix(s, "https://github.com/")
	s = strings.TrimPrefix(s, "http://github.com/")

	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}

	parts := strings.Split(s, "/")

	if len(parts) >= 2 {
		return parts[0], strings.TrimSuffix(parts[1], ".git")
	}

	return "", ""
}

func escapePathSegments(p string) string {
	parts := strings.Split(p, "/")

	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}

	return strings.Join(parts, "/")
}

func fetchGitHubContentRaw(spec repoSpec, pathInRepo, token string) ([]byte, error) {
	owner := url.PathEscape(spec.Owner)
	repo := url.PathEscape(spec.Repo)
	esc := escapePathSegments(pathInRepo)

	base := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, esc)

	u, _ := url.Parse(base)
	q := u.Query()

	if spec.Ref != "" {
		q.Set("ref", spec.Ref)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("User-Agent", "course-tests-fetcher/3.0")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository path not found: %s", pathInRepo)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(string(slurp)))
	}

	return io.ReadAll(resp.Body)
}

func safeJoin(root, p string) (string, error) {
	if filepath.IsAbs(p) {
		return "", errors.New("absolute paths is restricted")
	}

	clean := filepath.Clean(p)
	dst := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, dst)

	if err != nil {
		return "", err
	}

	if strings.HasPrefix(rel, "..") || rel == ".." {
		return "", errors.New("the path goes beyond the repository")
	}

	return dst, nil
}

func relOrSame(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}
