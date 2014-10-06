package main

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) {
	TestingT(t)
}

type PackageInfoSuite struct{}

var _ = Suite(&PackageInfoSuite{})

func (s *PackageInfoSuite) TestPackageInfoFilenameParsing(c *C) {
	testData := map[string]PackageInfo{
		"foo_1.0.tar.gz": {
			StageFile:   "foo_1.0.tar.gz",
			Name:        "foo",
			Version:     "1.0",
			Compression: "gz",
			Uid:         0,
		},
		"/tmp/foo/bar/bar_1.0.4.5.tar.xz": {
			StageFile:   "/tmp/foo/bar/bar_1.0.4.5.tar.xz",
			Name:        "bar",
			Version:     "1.0.4.5",
			Compression: "xz",
			Uid:         1,
		},
	}

	var i uint32 = 0
	for filename, expected := range testData {
		info := NewPackageInfo(filename, i)
		if c.Check(info, NotNil) == false {
			continue
		}
		c.Check(info.StageFile, Equals, expected.StageFile)
		c.Check(info.Name, Equals, expected.Name)
		c.Check(info.Version, Equals, expected.Version)
		c.Check(info.Compression, Equals, expected.Compression)
		c.Check(info.Uid, Equals, expected.Uid)
		i = i + 1
	}
}
