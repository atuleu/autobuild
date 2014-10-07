package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

type CommandGitPreparePackage struct {
}

func (x *CommandGitPreparePackage) updateChangelog(name string, version string) error {
	f, err := os.Open("debian/changelog")

	if err != nil {
		return err
	}

	changelog, err := ioutil.ReadAll(f)
	f.Close()

	if err != nil {
		return err
	}

	date := time.Now().Format(time.RFC1123Z)
	user, err := RunOutputCommand("git", "config", "--get", "user.name")

	if err != nil {
		return err
	}

	users := strings.TrimSpace(string(user))

	email, err := RunOutputCommand("git", "config", "--get", "user.email")

	if err != nil {
		return err
	}

	emails := strings.TrimSpace(string(email))

	ch := fmt.Sprintf("%s (%s-1) UNRELEASED; urgency=low\n\n  * \n\n -- %s <%s>  %s\n\n",
		name, version, users, emails, date) + string(changelog)

	f, err = os.Create("debian/changelog")

	if err != nil {
		return err
	}

	f.Write([]byte(ch))
	f.Close()

	editor := os.Getenv("EDITOR")

	if len(editor) == 0 {
		editor = "vim"
	}

	cmd := MakeInheritedCommand(editor, "debian/changelog")

	if err := cmd.Run(); err != nil {
		return err
	}

	if err := RunCommand("git", "add", "debian/changelog"); err != nil {
		return err
	}

	cmd = MakeInheritedCommand("git", "commit", "-e", "-m", fmt.Sprintf("Release version %s", version))

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func (x *CommandGitPreparePackage) readDebianBranches() ([]string, error) {
	f, err := os.Open(".git/refs/heads")

	if err != nil {
		return nil, err
	}

	names, err := f.Readdirnames(0)

	if err != nil {
		return nil, err
	}

	ret := make([]string, 0)

	for _, n := range names {
		if strings.HasPrefix(n, "debian-") {
			ret = append(ret, n[7:])
		}
	}

	return ret, nil
}

var origTarballRegexp *regexp.Regexp
var origVersionRegexp *regexp.Regexp
var origCompressionRegexp *regexp.Regexp

type OrigInfo struct {
	Name        string
	Version     string
	Compression string
}

func NewOrigInfoFromPath(pathname string) (*OrigInfo, error) {
	basename := path.Base(pathname)

	matched := origTarballRegexp.FindStringSubmatch(basename)
	if matched == nil {
		return nil, fmt.Errorf("Wrong package filename `%s'", basename)
	}

	if origVersionRegexp.MatchString(matched[2]) == false {
		return nil, fmt.Errorf("Wrong package version `%s'", matched[2])
	}

	if origCompressionRegexp.MatchString(matched[5]) == false {
		return nil, fmt.Errorf("Wrong package compression `%s'", matched[5])
	}

	return &OrigInfo{
		Name:        matched[1],
		Version:     matched[2],
		Compression: matched[5],
	}, nil
}

func (x *CommandGitPreparePackage) Execute(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("Please provide one tarball of a package (e.g. made with 'make distcheck')")
	}

	oInfo, err := NewOrigInfoFromPath(args[0])
	if err != nil {
		return err
	}

	f, err := os.Open("debian/changelog")

	if err != nil {
		return err
	}

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')

	if err != nil {
		return err
	}

	f.Close()

	chm, _ := regexp.Compile(`^([^\s]+)\s+\(([0-9]+(\.[0-9]+)+)-[0-9]\)`)
	matched := chm.FindStringSubmatch(line)

	if matched == nil {
		return fmt.Errorf("Failed to extract version information from debian changelog")
	}

	if matched[2] != oInfo.Version {
		if err := x.updateChangelog(matched[1], oInfo.Version); err != nil {
			return err
		}
	}

	tmp, err := ioutil.TempDir("", "git-prepare-package")

	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)

	var tp string

	switch oInfo.Compression {
	case "gz":
		tp = "z"
	case "bz2":
		tp = "j"
	case "xz":
		tp = "J"
	}

	// Extract
	err = RunCommand("tar", "-C", tmp, fmt.Sprintf("-x%sf", tp), args[0])

	if err != nil {
		return err
	}

	nm := fmt.Sprintf("%s_%s", oInfo.Name, oInfo.Version)
	orig := fmt.Sprintf("%s.orig.tar.gz", nm)
	diff := fmt.Sprintf("%s.diff", nm)
	diffgz := fmt.Sprintf("%s.gz", diff)
	dname := fmt.Sprintf("%s-%s", oInfo.Name, oInfo.Version)

	err = RunCommand("tar", "-C", tmp, "-czf", path.Join(tmp, orig), dname)

	if err != nil {
		return err
	}

	os.RemoveAll(dname)
	os.Mkdir(path.Join(tmp, "patches"), 0755)

	branches, err := x.readDebianBranches()

	if err != nil {
		return err
	}

	files := make([]string, 0)

	for _, branch := range branches {
		if options.Verbose {
			fmt.Printf("Generating patches for `%s'\n", branch)
		}

		cmd := MakeCommand("git", "diff", fmt.Sprintf("debian..debian-%s", branch))
		xz := MakeCommand("xz", "-z", "-c")

		xz.Stdout, _ = os.Create(fmt.Sprintf("%s/patches/%s.diff.xz", tmp, branch))
		xz.Stdin, _ = cmd.StdoutPipe()

		if err := cmd.Start(); err != nil {
			return err
		}

		if err := xz.Start(); err != nil {
			return err
		}

		if err := cmd.Wait(); err != nil {
			return err
		}

		if err := xz.Wait(); err != nil {
			return err
		}

		files = append(files, fmt.Sprintf("patches/%s.diff.xz", branch))
	}

	rd, err := os.Open("debian/autobuild/options")

	if err == nil {
		wr, err := os.Create(path.Join(tmp, "options"))

		if err != nil {
			rd.Close()
			return err
		}

		io.Copy(wr, rd)

		wr.Close()
		rd.Close()

		files = append(files, "options")
	}

	diffbase, err := RunOutputCommand("git", "merge-base", "master", "debian")

	if err != nil {
		return err
	}

	b := strings.TrimRight(string(diffbase), "\n")

	diffcmd := MakeCommand("git", "diff", fmt.Sprintf("%s..debian", b))
	diffcmd.Stdout = nil

	gzipcmd := MakeCommand("gzip", "-c")

	gzipcmd.Stdin, _ = diffcmd.StdoutPipe()
	gzipcmd.Stdout, _ = os.Create(path.Join(tmp, diffgz))

	if err := diffcmd.Start(); err != nil {
		return err
	}

	if err := gzipcmd.Start(); err != nil {
		return err
	}

	if err := diffcmd.Wait(); err != nil {
		return err
	}

	if err := gzipcmd.Wait(); err != nil {
		return err
	}

	targs := []string{
		"-C",
		tmp,
		"-cJf",
		fmt.Sprintf("%s.tar.xz", nm),
		orig,
		diffgz,
	}

	targs = append(targs, files...)
	cmd := MakeInheritedCommand("tar", targs...)

	if err := cmd.Run(); err != nil {
		return err
	}

	fmt.Printf("Generated %s.tar.xz\n", nm)

	return nil
}

func init() {
	var err error
	origTarballRegexp, err = regexp.Compile(`\A([a-z][a-z0-9\-]*)[-_]([\.0-9]+(~[a-z0-9]+)?)(\.orig)?\.tar\.([a-z0-9]+)\z`)
	if err != nil {
		panic(err)
	}
	origVersionRegexp, err = regexp.Compile(`\A[0-9]+(\.[0-9]+)*(~[a-z0-9]+)?\z`)
	if err != nil {
		panic(err)
	}
	origCompressionRegexp, err = regexp.Compile(`\A(bz2|xy|gz)\z`)
	if err != nil {
		panic(err)
	}
	parser.AddCommand("git-prepare-package",
		"Prepare a package using information from a git repository",
		"The git-prepare-package command creates an archive suitable for use with `autobuild stage' from a git repository. It expects the original tarball source as an argument and uses the `debian' branch of the current git repository to create the debian diff. Additionally, it will generate a new debian changelog entry if necessary.",
		&CommandGitPreparePackage{})
}
