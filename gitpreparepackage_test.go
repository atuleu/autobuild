package main

import (
	"fmt"

	. "gopkg.in/check.v1"
)

type GitPrepareSuite struct{}

var _ = Suite(&GitPrepareSuite{})

func (s *GitPrepareSuite) TestOrigTarballParsing(c *C) {
	expectedData := map[string]OrigInfo{
		"foo_1.2.3.tar.gz": {
			Name:        "foo",
			Version:     "1.2.3",
			Compression: "gz",
		},
		"bar_1.2.3~dev1.orig.tar.bz2": {
			Name:        "bar",
			Version:     "1.2.3~dev1",
			Compression: "bz2",
		},
		"../some/valid/path/blah_0.1.tar.xy": {
			Name:        "blah",
			Version:     "0.1",
			Compression: "xy",
		},
		"very-complex-name-0.1.2.orig.tar.gz": {
			Name:        "very-complex-name",
			Version:     "0.1.2",
			Compression: "gz",
		},
	}

	for filepath, expected := range expectedData {
		info, err := NewOrigInfoFromPath(filepath)
		if c.Check(info, NotNil) == false {
			fmt.Printf("Failed to parse `%s', got %s\n", filepath, err)
			continue
		}
		c.Check(err, IsNil)
		c.Check(info.Name, Equals, expected.Name)
		c.Check(info.Version, Equals, expected.Version)
		c.Check(info.Compression, Equals, expected.Compression)
	}
}

func (s *GitPrepareSuite) TestOrigTarballParsingFailure(c *C) {
	expectedData := map[string]string{
		"1badName2_0.1.2.tar.gz":        "Wrong package filename `1badName2_0.1.2.tar.gz'",
		"good-name2_.1~invalid1.tar.gz": "Wrong package version `.1~invalid1'",
		"good-name2-0.1.2~good.tar.zip": "Wrong package compression `zip'",
	}

	for filepath, expectedError := range expectedData {
		info, err := NewOrigInfoFromPath(filepath)
		c.Check(info, IsNil)
		if c.Check(err, NotNil) == false {
			continue
		}
		c.Check(err.Error(), Equals, expectedError)
	}
}
