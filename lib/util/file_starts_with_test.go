package util

import (
	"os"
	"path"
	"testing"
)

func TestFileStartsWithGood(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("test1 ")); err != nil {
		t.Fatalf(`FileStartsWith("test1 ") = %v, want: nil`, err)
	}
}

func TestFileStartsNotEqual(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("test2 ")); err != ErrFileStartsWithNotEqual {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithNotEqual)
	}
}

func TestFileStartsDirectory(t *testing.T) {
	tmp_file := t.TempDir()

	if err := FileStartsWith(tmp_file, []byte("test2 ")); err != ErrFileStartsWithDirectory {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithDirectory)
	}
}

func TestFileStartsSmall(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("small file\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("biiiiiiiiiig prefix")); err != ErrFileStartsWithFileTooSmall {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithFileTooSmall)
	}
}
