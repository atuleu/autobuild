package main

import (
	"bufio"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"regexp"
	"strings"
)

type CommandDscPreparePackage struct {
}

type DscFileInfo struct {
	Source        string
	OrigVersion   string
	DebianVersion string
	Files         []string
	Sha1          map[string][]byte
	Sha256        map[string][]byte
}

func sliceContains(value string, s []string) bool {
	for _, v := range s {
		if v == value {
			return true
		}
	}
	return false
}

func equalHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}

	return true
}

func (info *DscFileInfo) parseSource(value string) error {
	if len(info.Source) != 0 {
		return fmt.Errorf("Could not set Source to %s, it is already %s", value, info.Source)
	}
	info.Source = value
	return nil
}

func (info *DscFileInfo) parseVersion(value string) error {
	if len(info.OrigVersion) != 0 {
		return nil
	}
	versionRegexp, err := regexp.Compile(`([0-9]+(\.[0-9]+)*)-([0-9a-zA-Z~]+)`)
	if err != nil {
		return err
	}

	matches := versionRegexp.FindStringSubmatch(value)
	if matches == nil {
		return fmt.Errorf("Invalid version syntax %s", value)
	}

	info.OrigVersion = matches[1]
	info.DebianVersion = matches[3]
	return nil

}

func (info *DscFileInfo) parseFiles(value string) error {
	data := strings.Split(value, " ")
	if len(data) != 3 {
		return fmt.Errorf("Bad formatted sha1 line %s", value)
	}
	if sliceContains(data[2], info.Files) == false {
		info.Files = append(info.Files, data[2])
	}
	return nil
}

func (info *DscFileInfo) parseSha1(value string) error {
	data := strings.Split(value, " ")
	if len(data) != 3 {
		return fmt.Errorf("Bad formatted sha1 line %s", value)
	}
	if sliceContains(data[2], info.Files) == false {
		info.Files = append(info.Files, data[2])
	}

	cs, err := hex.DecodeString(data[0])
	if err != nil {
		return err
	}
	info.Sha1[data[2]] = cs

	return nil
}

func (info *DscFileInfo) parseSha256(value string) error {
	data := strings.Split(value, " ")
	if len(data) != 3 {
		return fmt.Errorf("Bad formatted sha1 line %s", value)
	}
	if sliceContains(data[2], info.Files) == false {
		info.Files = append(info.Files, data[2])
	}

	cs, err := hex.DecodeString(data[0])
	if err != nil {
		return err
	}
	info.Sha256[data[2]] = cs

	return nil
}

type valueParser func(info *DscFileInfo, value string) error

func (info *DscFileInfo) SetDscInfo(key, value string) error {
	parsers := map[string]valueParser{
		"Source":           (*DscFileInfo).parseSource,
		"Files":            (*DscFileInfo).parseFiles,
		"Version":          (*DscFileInfo).parseVersion,
		"Checksums-Sha1":   (*DscFileInfo).parseSha1,
		"Checksums-Sha256": (*DscFileInfo).parseSha256,
	}

	if p, ok := parsers[key]; ok == true {
		if err := p(info, value); err != nil {
			return err
		}
	}
	return nil
}

func ParseDscFile(r io.Reader) (*DscFileInfo, error) {
	rr := bufio.NewReader(r)

	var lastVariable string = ""

	res := &DscFileInfo{
		Sha1:   make(map[string][]byte),
		Sha256: make(map[string][]byte),
	}

	singleLineRegex, err := regexp.Compile(`\A([0-9A-Za-z\-]+): ([^\s].*)\n\z`)
	if err != nil {
		return nil, err
	}
	beginLineRegex, err := regexp.Compile(`\A([0-9A-Za-z\-]+):\s*\n\z`)
	if err != nil {
		return nil, err
	}

	followLineRegex, err := regexp.Compile(`\A ([^\s].*)\n\z`)
	if err != nil {
		return nil, err
	}
	for {
		l, err := rr.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		sLine := singleLineRegex.FindStringSubmatch(l)
		if sLine != nil {
			if err = res.SetDscInfo(sLine[1], sLine[2]); err != nil {
				return nil, err
			}
			lastVariable = ""
			continue
		}

		bLine := beginLineRegex.FindStringSubmatch(l)
		if bLine != nil {
			lastVariable = bLine[1]
			continue
		}

		fLine := followLineRegex.FindStringSubmatch(l)
		if fLine != nil {
			if len(lastVariable) == 0 {
				return nil, fmt.Errorf("File syntax error, find subvalue %s without any variable", fLine[0])
			}
			res.SetDscInfo(lastVariable, fLine[1])
			continue
		}

		lastVariable = ""
	}

	if len(res.Source) == 0 {
		return nil, fmt.Errorf("Missing Source package name in file")
	}

	origFileName := fmt.Sprintf("%s_%s.orig.tar.gz", res.Source, res.OrigVersion)
	debianFileName := fmt.Sprintf("%s_%s-%s.debian.tar.gz", res.Source, res.OrigVersion, res.DebianVersion)

	badFileError := fmt.Errorf("Expected two file in .dsc, named %s and %s got %s",
		origFileName,
		debianFileName,
		res.Files)

	if len(res.Files) != 2 {
		return nil, badFileError
	}
	if res.Files[0] != origFileName || res.Files[1] != debianFileName {
		return nil, badFileError
	}

	return res, nil

}

func checkHash(filename string, hasher hash.Hash, expected []byte) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(hasher, file); err != nil {
		return err
	} else {
		sum := hasher.Sum(nil)
		if equalHash(sum, expected) == false {
			return fmt.Errorf("Mismatched sha1 checksum for %s, got %s expected %s",
				filename,
				hex.EncodeToString(sum),
				hex.EncodeToString(expected))
		}
	}
	return nil
}

func (info *DscFileInfo) CheckFilesChecksums() error {
	for _, f := range info.Files {
		if err := checkHash(f, sha1.New(), info.Sha1[f]); err != nil {
			return err
		}
		if err := checkHash(f, sha256.New(), info.Sha256[f]); err != nil {
			return err
		}

	}
	return nil
}

func (x *CommandDscPreparePackage) Execute(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("Please provide one .dsc file")
	}

	f, err := os.Open(args[0])
	defer f.Close()
	if err != nil {
		return err
	}

	dscInfo, err := ParseDscFile(f)

	if err != nil {
		return err
	}

	if err = dscInfo.CheckFilesChecksums(); err != nil {
		return err
	}

	return fmt.Errorf("Not yet implemented")
}

func init() {
	parser.AddCommand("dsc-prepare-package",
		"Prepare a package using information from a .dsc file",
		"The dsc-prepare-package command creates an archive suitable to use with `autobuild stage' from a debian .dsc fle. It expect the .dsc file to be in the same directory than the .orig and .debian archive.",
		&CommandDscPreparePackage{})
}
