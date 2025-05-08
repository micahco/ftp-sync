package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

var HelpArgs = []string{"help", "--help", "-h"}

func main() {
	user := flag.String("user", "anonymous", "FTP user")
	password := flag.String("password", "", "FTP password")

	flag.Usage = func() {
		fmt.Println("usage: ftp-sync [options] <host:port> <source> <dest>")
		fmt.Println("options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	addr := args[0]
	source := args[1]
	dest := args[2]

	// Ensure dest has trailing slash for consistent path handling
	if !strings.HasSuffix(dest, "/") {
		dest += "/"
	}

	// Connect to FTP server
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(5*time.Second), ftp.DialWithDisabledUTF8(true))
	if err != nil {
		fatal(err)
	}
	defer conn.Quit()

	if err := conn.Login(*user, *password); err != nil {
		fatal(err)
	}

	fmt.Println("scanning...")
	seen, err := traverse(conn, dest)
	if err != nil {
		fatal(err)
	}

	uploaded := 0

	err = filepath.WalkDir(source, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}

			remotePath := dest + rel
			if _, ok := seen[remotePath]; !ok {
				if err := upload(conn, path, remotePath); err != nil {
					return err
				}

				fmt.Println(remotePath)

				uploaded++
			}
		}

		return nil
	})
	if err != nil {
		fatal(err)
	}

	if uploaded == 0 {
		fmt.Println("no changes")
	}
}

func traverse(conn *ftp.ServerConn, dir string) (map[string]bool, error) {
	seen := make(map[string]bool)

	var inorder func(path string) error
	inorder = func(path string) error {
		if err := conn.ChangeDir(path); err != nil {
			return fmt.Errorf("failed to change to directory %s: %w", dir, err)
		}

		entries, err := conn.List(".")
		if err != nil {
			return fmt.Errorf("failed to list directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.Name == "." || entry.Name == ".." {
				continue
			}

			entryPath := filepath.Join(path, entry.Name)

			switch entry.Type {
			case ftp.EntryTypeFile:
				if _, ok := seen[entryPath]; !ok {
					seen[entryPath] = true
				}
			case ftp.EntryTypeFolder:
				if err := inorder(entryPath); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := inorder(dir); err != nil {
		return nil, err
	}

	return seen, nil
}

func upload(conn *ftp.ServerConn, local, remote string) error {
	file, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", local, err)
	}
	defer file.Close()

	dir := filepath.Dir(remote)
	err = conn.MakeDir(dir)
	if err != nil && err.Error() != "550 Already exists" {
		return fmt.Errorf("failed to change to directory %s: %w", dir, err)
	}

	if err := conn.Stor(remote, file); err != nil {
		return fmt.Errorf("failed to store %s: %w", remote, err)
	}

	return nil
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
