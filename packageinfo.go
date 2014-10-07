package main

import (
	"fmt"
	"path"
	"regexp"
)

type PackageInfo struct {
	StageFile     string
	Name          string
	Version       string
	DebianVersion string
	Compression   string
	Uid           uint32
}

var debianizedVersionRegexp *regexp.Regexp = nil
var packageNameRegexp *regexp.Regexp = nil

func NewPackageInfo(filename string, uid uint32) (*PackageInfo, error) {
	basename := path.Base(filename)
	nameMatch := packageNameRegexp.FindStringSubmatch(basename)

	if nameMatch == nil {
		return nil, fmt.Errorf("Wrong package filename `%s'", basename)
	}

	versionMatch := debianizedVersionRegexp.FindStringSubmatch(nameMatch[2])
	if versionMatch == nil {
		return nil, fmt.Errorf("Wrong package version `%s'", nameMatch[2])
	}

	return &PackageInfo{
		StageFile:     filename,
		Name:          nameMatch[1],
		Version:       versionMatch[1],
		DebianVersion: versionMatch[5],
		Compression:   nameMatch[3],
		Uid:           uid,
	}, nil
}

func (x *PackageInfo) MatchStageFile(filename string) bool {
	if x == nil {
		return false
	}

	return path.Base(x.StageFile) == filename
}

func init() {
	packageNameRegexp, _ = regexp.Compile(`\A([a-z][0-9a-z\-]+)_([0-9]+[\.0-9\-~a-z]+)\.tar\.(gz|bz2|xz)\z`)
	debianizedVersionRegexp, _ = regexp.Compile(`\A([0-9]+(\.[0-9]+)+(~[0-9a-z]+)?)(-([0-9]+[a-z]+[0-9]+(~[0-9a-z]+)?))?\z`)
}
