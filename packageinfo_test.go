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
			StageFile:     "foo_1.0.tar.gz",
			Name:          "foo",
			Version:       "1.0",
			DebianVersion: "",
			Compression:   "gz",
			Uid:           0,
		},
		"/tmp/foo/bar/bar_1.0.4.5.tar.xz": {
			StageFile:     "/tmp/foo/bar/bar_1.0.4.5.tar.xz",
			Name:          "bar",
			Version:       "1.0.4.5",
			DebianVersion: "",
			Compression:   "xz",
			Uid:           1,
		},
		"complex-version_1.0.0~dev1.tar.bz2": {
			StageFile:     "complex-version_1.0.0~dev1.tar.bz2",
			Name:          "complex-version",
			Version:       "1.0.0~dev1",
			DebianVersion: "",
			Compression:   "bz2",
			Uid:           2,
		},
		"/foo/blah_1.0~dev1-0truc1~backport2.tar.xz": {
			StageFile:     "/foo/blah_1.0~dev1-0truc1~backport2.tar.xz",
			Name:          "blah",
			Version:       "1.0~dev1",
			DebianVersion: "0truc1-backport2",
			Compression:   "xz",
			Uid:           3,
		},
	}

	for filename, expected := range testData {
		info, err := NewPackageInfo(filename, expected.Uid)
		if c.Check(err, IsNil) == false {
			continue
		}
		if c.Check(info, NotNil) == false {
			continue
		}
		c.Check(info.StageFile, Equals, expected.StageFile)
		c.Check(info.Name, Equals, expected.Name)
		c.Check(info.Version, Equals, expected.Version)
		c.Check(info.Compression, Equals, expected.Compression)
		c.Check(info.Uid, Equals, expected.Uid)
	}
}

func (s *PackageInfoSuite) TestPackageInfoFireErrors(c *C) {
	expectedData := map[string]string{
		"1badName_0.1.2-2.tar.gz":                  "Wrong package filename `1badName'",
		"good-name42_0.1.2~badVersion2.1-1ubuntu1": "Wrong package version `0.1.2~badVersion2.1-1ubuntu1'",
	}

	for filename, expectedError := range expectedData {
		info, err := NewPackageInfo(filename, 0)
		if c.Check(info, IsNil) {
			continue
		}

		if c.Check(err, NotNil) {
			continue
		}

		c.Check(err.Error(), Equals, expectedError)

	}
}
