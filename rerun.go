// Copyright 2013 The rerun AUTHORS. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"time"

	"go/build"
)

var (
	do_tests = flag.Bool("test", false, "Run tests (before running program)")
	do_build = flag.Bool("build", false, "Build program")
	ignore   = flag.Bool("no-git", true, "ignore .git directory")
	watch    = flag.String("watch", "", "root directory to watch")
)

func buildpathDir(buildpath string) (string, error) {
	pkg, err := build.Import(buildpath, "", 0)

	if err != nil {
		return "", err
	}

	if pkg.Goroot {
		return "", err
	}

	return pkg.Dir, nil
}

type scanCallback func(path string)

func scanChanges(path string, cb scanCallback) {
	log("watching: %s", path)

	last := time.Now()

	for {
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if *ignore && info.IsDir() && p == filepath.Join(path, ".git") {
				return filepath.SkipDir
			}

			if info.ModTime().After(last) {
				cb(path)
				last = time.Now()
			}
			return nil
		})

		time.Sleep(500 * time.Millisecond)
	}
}

func log(format string, args ...interface{}) {
	fmt.Printf("[rerun] %s", fmt.Sprintf(format+"\n", args...))
}

func gobuild(buildpath string) (bool, error) {
	cmd := exec.Command("go", "build", "-v", buildpath)

	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Run(); err != nil {
		log("build failed")
		fmt.Println(buf.String())
		return false, err
	}

	log("build succeeded")
	return true, nil
}

func goinstall(buildpath string) (bool, error) {
	cmd := exec.Command("go", "get", buildpath)

	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Run(); err != nil {
		log("install failed")
		fmt.Println(buf.String())
		return false, err
	}

	log("install succeeded")
	return true, nil
}

func gotest(buildpath string) (bool, error) {
	cmd := exec.Command("go", "test", "-v", buildpath)

	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Run(); err != nil {
		log("tests failed")
		fmt.Println(buf.String())
		return false, err
	}

	log("tests passed")
	return true, nil
}

func run(ch chan bool, bin string, args []string) {
	go func() {
		var proc *os.Process

		for relaunch := range ch {
			if proc != nil {
				if err := proc.Signal(os.Interrupt); err != nil {
					proc.Kill()
				}
				proc.Wait()
			}

			if !relaunch {
				continue
			}

			cmd := exec.Command(bin, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				log("error: %s", err)
			}

			proc = cmd.Process
		}
	}()
	return
}

func refresh(buildpath string, ch chan bool) {
	if *do_tests {
		if ok, _ := gotest(buildpath); !ok {
			ch <- false
			return
		}
	}

	if *do_build {
		if ok, _ := gobuild(buildpath); !ok {
			ch <- false
			return
		}
	}

	if ok, _ := goinstall(buildpath); !ok {
		ch <- false
		return
	}

	ch <- true
	return
}

func rerun(buildpath string, args []string) (err error) {
	pkg, err := build.Import(buildpath, "", 0)
	if err != nil {
		return
	}

	if pkg.Name != "main" {
		err = errors.New(fmt.Sprintf("expected package %q, got %q", "main", pkg.Name))
		return
	}

	_, name := path.Split(buildpath)
	bin := filepath.Join(pkg.BinDir, name)

	ch := make(chan bool)
	go run(ch, bin, args)

	refresh(buildpath, ch)

	dir, err := buildpathDir(buildpath)
	if err != nil {
		return
	}

	// watch alternate dir
	if watch != nil && *watch != "" {
		dir = *watch
	}

	scanChanges(dir, func(path string) {
		log("change detected")
		refresh(buildpath, ch)
	})

	return
}

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: rerun [--no-git] [--test] [--no-run] [--build] [--race] <import path> [arg]*")
		os.Exit(1)
	}

	if *ignore {
		log("ignoring .git dir")
	}

	buildpath := flag.Args()[0]
	args := flag.Args()[1:]

	if err := rerun(buildpath, args); err != nil {
		log("error: %s", err)
	}
}
