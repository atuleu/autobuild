package main

import (
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"io"
	"bufio"
	"strings"
	"sync"
	"encoding/gob"
	"encoding/json"
	"bytes"
)

type DistroBuildInfo struct {
	IncomingDir string
	Changes     string
	Distribution Distribution
	ChangesFiles []string
	Files       []string
	Log      *bytes.Buffer
	Error    error
}

type BuildInfo struct {
	Info    *PackageInfo
	Package *ExtractedPackage

	BuildResultsDir string
	Error error

	Source   map[string]*DistroBuildInfo
	Binaries map[string]*DistroBuildInfo
}

type ExtractedPackage struct {
	Dir     string
	OrigGz  string
	DiffGz  string
	Patches map[string]string
	Options BuildOptions
}

type PackageBuilder struct {
	CurrentlyBuilding *PackageInfo
	FinishedPackages []*BuildInfo
	PackageQueue []*PackageInfo

	notifyQueue chan bool

	Mutex sync.Mutex
}

var builder = PackageBuilder {
	notifyQueue: make(chan bool, 1024),
}

var packageInfoRegex *regexp.Regexp
var changelogSubstituteRegex *regexp.Regexp

func (x *PackageBuilder) Do(fn func (b *PackageBuilder) error) error {
	x.Mutex.Lock()
	err := fn(x)
	x.Mutex.Unlock()

	return err
}

func (x *PackageBuilder) Stage(pname string, fn func (x *PackageBuilder) (*PackageInfo, error)) error {
	return x.Do(func (b *PackageBuilder) error {
		// Check if we are currently building this package
		if b.CurrentlyBuilding.MatchStageFile(pname) {
			return fmt.Errorf("The file `%s' is currently building. Please wait until the built is finished to build the package again.", pname)
		}

		// Check results
		for _, binfo := range b.FinishedPackages {
			if binfo.Info.MatchStageFile(pname) {
				return fmt.Errorf("The file `%s' has already been built and is waiting to be released. Use `autobuild release' to release or discard before building again.")
			}
		}

		// Check queue
		for _, info := range b.PackageQueue {
			if info.MatchStageFile(pname) {
				return fmt.Errorf("The file `%s' has already been queued to be built. Use `autobuild queue' to remove the queued package first.")
			}
		}

		info, err := fn(b)

		if err != nil {
			return err
		}

		b.PackageQueue = append(b.PackageQueue, info)
		b.notifyQueue <- true

		return nil
	})
}

func (x *PackageBuilder) Run() {
	for {
		select {
		case _ = <- x.notifyQueue:
			if x.CurrentlyBuilding == nil {
				x.Do(func (b *PackageBuilder) error {
					if len(b.PackageQueue) > 0 {
						b.CurrentlyBuilding = b.PackageQueue[0]
						b.PackageQueue = b.PackageQueue[1:]
					}

					return nil
				})
			}

			if x.CurrentlyBuilding != nil {
				binfo := x.buildPackage()

				if options.Verbose {
					fmt.Printf("Finished building `%s'\n", path.Base(binfo.Info.StageFile))
				}

				x.Do(func (b *PackageBuilder) error {
					b.FinishedPackages = append(b.FinishedPackages, binfo)
					b.CurrentlyBuilding = nil

					if len(b.PackageQueue) > 0 {
						b.notifyQueue <- true
					}

					return nil
				})
			}
		}
	}
}

func (x *PackageBuilder) extractPackage(info *PackageInfo) (*ExtractedPackage, error) {
	if options.Verbose {
		fmt.Printf("Extracting package `%s'...\n", info.Name)
	}

	tmp := path.Join(options.Base, "tmp")
	os.MkdirAll(tmp, 0755)

	tdir, err := ioutil.TempDir(tmp, "autobuild")

	if err != nil {
		return nil, fmt.Errorf("Failed to create temporary directory to extract package: %s", err)
	}

	z := "gz"

	switch info.Compression {
	case "xz":
		z = "J"
	case "bz2":
		z = "j"
	}

	// Extract archive
	if err := RunCommandIn(tdir, "tar", "-x"+z+"f", info.StageFile); err != nil {
		os.RemoveAll(tdir)
		return nil, fmt.Errorf("Failed to extract staged package `%s': %s", path.Base(info.StageFile), err)
	}

	// Look for options
	bopts := options.BuildOptions

	f, err := os.Open(path.Join(tdir, "options"))

	if err == nil {
		if options.Verbose {
			fmt.Printf("Parsing package options...\n")
		}

		dec := json.NewDecoder(f)
		dec.Decode(&bopts)
		f.Close()
	}

	if options.Verbose {
		fmt.Printf("Checking for %s_%s.orig.tar.gz...\n", info.Name, info.Version)
	}

	origgz := path.Join(tdir, fmt.Sprintf("%s_%s.orig.tar.gz", info.Name, info.Version))

	if _, err := os.Stat(origgz); err != nil {
		os.RemoveAll(tdir)

		return nil, fmt.Errorf("The stage file `%s' does not contain the original tarball `%s'",
		                       path.Base(info.StageFile), path.Base(origgz))
	}

	if options.Verbose {
		fmt.Printf("Checking for %s_%s.diff.gz...\n", info.Name, info.Version)
	}

	diffgz := path.Join(tdir, fmt.Sprintf("%s_%s.diff.gz", info.Name, info.Version))

	if _, err := os.Stat(origgz); err != nil {
		os.RemoveAll(tdir)

		return nil, fmt.Errorf("The stage file `%s' does not contain the debian diff `%s'",
		                       path.Base(info.StageFile), path.Base(diffgz))
	}

	if options.Verbose {
		fmt.Printf("Extracting additional patches...\n")
	}

	// Extract patches
	patchdir := path.Join(tdir, "patches")
	patches := make(map[string]string, 0)

	if f, err := os.Open(patchdir); err == nil {
		names, _ := f.Readdirnames(0)

		for _, name := range names {
			fullname := path.Join(patchdir, name)
			ext := path.Ext(name)
			name = name[0 : len(name)-len(ext)]

			switch ext {
			case ".xz":
				RunCommandIn(tdir, "unxz", fullname)
			case ".gz":
				RunCommandIn(tdir, "gunzip", fullname)
			case ".bz2":
				RunCommandIn(tdir, "bunzip2", fullname)
			default:
			}

			patches[path.Base(name)] = path.Join(patchdir, name)
		}
	}

	return &ExtractedPackage{
		Dir:     tdir,
		OrigGz:  origgz,
		DiffGz:  diffgz,
		Patches: patches,
		Options: bopts,
	}, nil
}

func (x *PackageBuilder) substituteUnreleased(changelog string, distro *Distribution) error {
	// Read complete file
	b, err := ioutil.ReadFile(changelog)

	if err != nil {
		return fmt.Errorf("Failed to open debian/changelog for substitution: %s", err)
	}

	repl := fmt.Sprintf("-${1}%s0) %s", distro.CodeName, distro.CodeName)

	// Substitute
	ret := changelogSubstituteRegex.ReplaceAllString(string(b), repl)

	// Write changelog back
	if err := ioutil.WriteFile(changelog, []byte(ret), 0644); err != nil {
		return fmt.Errorf("Failed to write debian/changelog for substitution: %s", err)
	}

	return nil
}

func (x *PackageBuilder) readLines(rd *bufio.Reader, fn func (line string) error) error {
	for {
		line, err := rd.ReadString('\n')

		if err != nil && err != io.EOF {
			return err
		}

		if err == nil {
			line = line[0:len(line)-1]
		}

		e := fn(line)

		if e != nil {
			return e
		} else if err != nil {
			return err
		}
	}

	return nil
}

func (x *PackageBuilder) parseChanges(info *DistroBuildInfo) error {
	f, err := os.Open(info.Changes + ".changes")

	if err != nil {
		return err
	}

	defer f.Close()

	rd := bufio.NewReader(f)

	err = x.readLines(rd, func (line string) error {
		line = strings.TrimSpace(line)

		if line == "Files:" {
			return io.EOF
		}

		return nil
	})

	if err != nil && err != io.EOF {
		return err
	}

	info.ChangesFiles = make([]string, 0)

	err = x.readLines(rd, func (line string) error {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 5)

		if len(parts) == 5 {
			file := path.Join(info.IncomingDir, parts[4])
			info.ChangesFiles = append(info.ChangesFiles, file)
		}

		return nil
	})

	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

func (x *PackageBuilder) moveResults(info *DistroBuildInfo, resdir string, rmfiles ...string) error {
	incoming := info.IncomingDir
	os.MkdirAll(incoming, 0755)

	ret := make([]string, 0)

	d, err := os.Open(resdir)

	if err != nil {
		return fmt.Errorf("Failed to open results directory `%s': %s",
		                  resdir,
		                  err)
	}

	names, err := d.Readdirnames(0)

	if err != nil {
		return fmt.Errorf("Failed to read build results from `%s': %s",
		                  resdir,
		                  err)
	}

	rmmapping := make(map[string]struct{})

	for _, rm := range rmfiles {
		rmmapping[rm] = struct{}{}
	}

	chsuf := ".changes"

	for _, name := range names {
		filename := path.Join(resdir, name)
		target := path.Join(incoming, name)

		// Remove filename if the target was already built
		if _, ok := rmmapping[target]; ok {
			os.Remove(filename)
		} else {
			if err := os.Rename(filename, target); err != nil {
				return fmt.Errorf("Failed to move build result `%s' to target location `%s': %s",
				                  filename,
				                  incoming,
				                  err)
			}

			if strings.HasSuffix(target, chsuf) {
				info.Changes = target[0:len(target)-len(chsuf)]

				if err := x.parseChanges(info); err != nil {
					return fmt.Errorf("Failed to parse changes file `%s': %s",
					                  info.Changes + ".changes",
					                  err)
				}
			}

			ret = append(ret, target)
		}
	}

	info.Files = ret
	return nil
}

func (x *PackageBuilder) extractSourcePackage(info *BuildInfo, distro *Distribution) error {
	pack := info.Package

	pkgdir := path.Join(pack.Dir, fmt.Sprintf("%s-%s", info.Info.Name, info.Info.Version))
	os.RemoveAll(pkgdir)

	if options.Verbose {
		fmt.Printf("Extracting: %v...\n", pack.OrigGz)
	}

	// Extract original orig.tar.gz
	if err := RunCommandIn(pack.Dir, "tar", "-xzf", pack.OrigGz); err != nil {
		return fmt.Errorf("Failed to extract original tarball `%s': %s",
		                  path.Base(pack.OrigGz), err)
	}

	// Check if it was extracted correctly
	if _, err := os.Stat(pkgdir); err != nil {
		return fmt.Errorf("Did not find original source `%s' after extract original tarball `%s'",
		                  path.Base(pkgdir),
		                  path.Base(pack.OrigGz))
	}

	if options.Verbose {
		fmt.Printf("Extracting debian diff: %v...\n", pack.DiffGz)
	}

	// Apply the diff.gz debian patch
	dgz, err := os.Open(pack.DiffGz)

	if err != nil {
		return fmt.Errorf("Failed to open debian diff `%s': %s",
		                  path.Base(pack.DiffGz), err)
	}

	defer dgz.Close()

	rd, err := gzip.NewReader(dgz)

	if err != nil {
		return fmt.Errorf("Failed to extract debian diff `%s': %s",
		                  path.Base(pack.DiffGz), err)
	}

	if options.Verbose {
		fmt.Printf("Patching...\n")
	}

	cmd := MakeCommandIn(pkgdir, "patch", "-p1")
	cmd.Stdin = rd

	cmd.Stdout = nil
	cmd.Stderr = nil

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Failed to run debian debian patch: %s",
		                  string(out))
	}

	dgz.Close()

	if _, err := os.Stat(path.Join(pkgdir, "debian")); err != nil {
		return fmt.Errorf("Could not find `debian' directory after applying debian patch")
	}

	// Apply distribution specific patches
	if patch, ok := pack.Patches[distro.CodeName]; ok {
		if options.Verbose {
			fmt.Printf("Apply distribution specific patch: %v...\n", patch)
		}

		cmd := MakeCommandIn(pkgdir, "patch", "-p1", "-i", patch)
		cmd.Stdout = nil
		cmd.Stderr = nil

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("Failed to apply distribution specific patch `%s': %s",
			                  path.Base(patch),
			                  string(out))
		}
	}

	if options.Verbose {
		fmt.Printf("Substitute ChangeLog...\n")
	}

	// Replace UNRELEASED in changelog with specific distro
	changelog := path.Join(pkgdir, "debian", "changelog")

	if err := x.substituteUnreleased(changelog, distro); err != nil {
		return err
	}

	return nil
}

func (x *PackageBuilder) buildSourcePackage(info *BuildInfo, distro *Distribution) error {
	src := &DistroBuildInfo{
		IncomingDir: path.Join(options.Base, "incoming", distro.Os, distro.CodeName),
		Log: &bytes.Buffer {},

		Distribution: Distribution {
			Os: distro.Os,
			CodeName: distro.CodeName,
			Architectures: []string {"source"},
		},
	}

	if options.Verbose {
		fmt.Printf("Building source package...\n")
	}

	src.Error = x.extractSourcePackage(info, distro)

	if src.Error != nil {
		info.Source[distro.SourceName()] = src
		return src.Error
	}

	pkgdir := path.Join(info.Package.Dir, fmt.Sprintf("%s-%s", info.Info.Name, info.Info.Version))

	// Call pdebuild
	cmd := MakeCommandIn(pkgdir,
		"pdebuild",
		"--pbuilder", options.Pbuilder,
		"--configfile", path.Join(options.Base, "etc", "pbuilderrc"),
		"--buildresult", info.BuildResultsDir,
		"--debbuildopts", "-us",
		"--debbuildopts", "-uc",
		"--debbuildopts", "-S")

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("DIST=%s/%s", distro.Os, distro.CodeName))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AUTOBUILD_BASE=%s", options.Base))

	var wr io.Writer

	if options.Verbose {
		wr = io.MultiWriter(src.Log, os.Stdout)
	} else {
		wr = src.Log
	}

	cmd.Stdout = wr
	cmd.Stderr = wr

	if options.Verbose {
		fmt.Printf("Run pdebuild for source in `%s'...\n", info.Package.Dir)
	}

	src.Error = cmd.Run()

	if src.Error != nil {
		os.RemoveAll(info.BuildResultsDir)
	} else {
		// Move build results to incoming
		x.moveResults(src, info.BuildResultsDir)
	}

	info.Source[distro.SourceName()] = src
	return src.Error
}

func (x *PackageBuilder) buildBinaryPackages(info *BuildInfo, distro *Distribution, arch string) error {
	bin := &DistroBuildInfo{
		IncomingDir: path.Join(options.Base, "incoming", distro.Os, distro.CodeName),

		Log: &bytes.Buffer {},

		Distribution: Distribution {
			Os: distro.Os,
			CodeName: distro.CodeName,
			Architectures: []string {arch},
		},
	}

	pkgdir := path.Join(info.Package.Dir, fmt.Sprintf("%s-%s", info.Info.Name, info.Info.Version))

	// Call pdebuild
	cmd := MakeCommandIn(pkgdir,
		"pdebuild",
		"--pbuilder", options.Pbuilder,
		"--configfile", path.Join(options.Base, "etc", "pbuilderrc"),
		"--buildresult", info.BuildResultsDir,
		"--debbuildopts", "-us",
		"--debbuildopts", "-uc",
		"--debbuildopts", "-b")

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("DIST=%s/%s", distro.Os, distro.CodeName))
	cmd.Env = append(cmd.Env, fmt.Sprintf("ARCH=%s", arch))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AUTOBUILD_BASE=%s", options.Base))

	var wr io.Writer

	if options.Verbose {
		wr = io.MultiWriter(bin.Log, os.Stdout)
	} else {
		wr = bin.Log
	}

	cmd.Stdout = wr
	cmd.Stderr = wr

	bin.Error = cmd.Run()

	if bin.Error != nil {
		os.RemoveAll(info.BuildResultsDir)
	} else {
		// Move build results to incoming (skipping source files)
		x.moveResults(bin, info.BuildResultsDir, info.Source[distro.SourceName()].Files...)
	}

	info.Binaries[distro.BinaryName(arch)] = bin
	return bin.Error
}

func (x *PackageBuilder) buildPackage() *BuildInfo {
	info := x.CurrentlyBuilding

	if options.Verbose {
		fmt.Printf("Building package %v (%v): %v\n", info.StageFile, info.Name, info.Version)
	}

	binfo := &BuildInfo{
		Info:       info,
		Source:     make(map[string]*DistroBuildInfo),
		Binaries:   make(map[string]*DistroBuildInfo),
	}

	pack, err := x.extractPackage(info)

	if err != nil {
		binfo.Error = err
		return binfo
	}

	buildresult := path.Join(pack.Dir, "result")

	os.RemoveAll(buildresult)
	os.MkdirAll(buildresult, 0755)

	binfo.BuildResultsDir = buildresult
	binfo.Package = pack

	defer os.RemoveAll(pack.Dir)

	// For each distribution
	for _, distro := range pack.Options.Distributions {
		if err := x.buildSourcePackage(binfo, distro); err != nil {
			return binfo
		}

		for _, arch := range distro.Architectures {
			x.buildBinaryPackages(binfo, distro, arch)
		}
	}

	return binfo
}

type PackageBuilderState struct {
	FinishedPackages []*BuildInfo
	PackageQueue []*PackageInfo
}

func (x *PackageBuilder) Save() {
	f := path.Join(options.Base, "run", "builder.state")

	x.Do(func (b *PackageBuilder) error {
		state := PackageBuilderState {
			FinishedPackages: b.FinishedPackages,
			PackageQueue: b.PackageQueue,
		}

		if b.CurrentlyBuilding != nil {
			state.PackageQueue = append([]*PackageInfo {b.CurrentlyBuilding}, state.PackageQueue...)
		}

		fn, err := os.Create(f)

		if err != nil {
			return err
		}

		enc := gob.NewEncoder(fn)

		if err := enc.Encode(state); err != nil {
			return err
		}
	
		fn.Close()
		return nil
	})
}

func (x *PackageBuilder) Load() {
	f := path.Join(options.Base, "run", "builder.state")

	x.Do(func (b *PackageBuilder) error {
		fn, err := os.Open(f)

		if err == nil {
			defer fn.Close()

			dec := gob.NewDecoder(fn)

			state := &PackageBuilderState {}
			err := dec.Decode(state)

			if err != nil {
				return err
			}

			b.FinishedPackages = state.FinishedPackages
			b.PackageQueue = state.PackageQueue

			if len(b.PackageQueue) > 0 {
				b.notifyQueue <- true
			}

			return nil
		}

		return err
	})
}

func init() {
	packageInfoRegex, _ = regexp.Compile(`(.*)[_-]([0-9]+(\.[0-9]+)+).tar.(gz|xz|bz2)`)
	changelogSubstituteRegex, _ = regexp.Compile(`-([0-9]+)\) UNRELEASED`)
}
