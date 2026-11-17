package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckImageTakesOnlyKeptPhotos(t *testing.T) {
	dir := t.TempDir()
	name := "up_" + strings.Repeat("0a", 16)
	good := []Image{
		{Path: filepath.Join(dir, name+".jpg"), Type: ImageJPEG},
		{Path: filepath.Join(dir, name+".png"), Type: ImagePNG},
		{Path: filepath.Join(dir, name+".webp"), Type: ImageWebP},
	}
	for _, im := range good {
		if err := CheckImage(im); err != nil {
			t.Errorf("CheckImage(%+v) = %v", im, err)
		}
	}
	if err := CheckImages(good); err != nil {
		t.Errorf("CheckImages = %v", err)
	}
	sep := string(filepath.Separator)
	bad := map[string]Image{
		"relative":             {Path: name + ".jpg", Type: ImageJPEG},
		"not clean":            {Path: dir + sep + "x" + sep + ".." + sep + name + ".jpg", Type: ImageJPEG},
		"trailing separator":   {Path: filepath.Join(dir, name+".jpg") + sep, Type: ImageJPEG},
		"type and ending":      {Path: filepath.Join(dir, name+".jpg"), Type: ImagePNG},
		"unknown type":         {Path: filepath.Join(dir, name+".gif"), Type: "image/gif"},
		"no type":              {Path: filepath.Join(dir, name+".jpg")},
		"the phone's name":     {Path: filepath.Join(dir, "IMG_0001.jpg"), Type: ImageJPEG},
		"short id":             {Path: filepath.Join(dir, "up_0a0a.jpg"), Type: ImageJPEG},
		"upper-case id":        {Path: filepath.Join(dir, "up_"+strings.Repeat("0A", 16)+".jpg"), Type: ImageJPEG},
		"double ending":        {Path: filepath.Join(dir, name+".jpg.exe"), Type: ImageJPEG},
		"a flag":               {Path: "--dangerously-skip-permissions", Type: ImageJPEG},
		"a name with a space":  {Path: filepath.Join(dir, name+" .jpg"), Type: ImageJPEG},
		"a name with new line": {Path: filepath.Join(dir, name+".jpg\n"), Type: ImageJPEG},
	}
	for what, im := range bad {
		if err := CheckImage(im); err == nil {
			t.Errorf("%s: CheckImage(%+v) took it", what, im)
		}
	}
	if err := CheckImages([]Image{good[0], good[0]}); err == nil {
		t.Error("the same photo twice was taken")
	}
	five := []Image{good[0], good[1], good[2],
		{Path: filepath.Join(dir, "up_"+strings.Repeat("1", 32)+".jpg"), Type: ImageJPEG},
		{Path: filepath.Join(dir, "up_"+strings.Repeat("2", 32)+".jpg"), Type: ImageJPEG}}
	if err := CheckImages(five); err == nil || CheckImages(five[:MaxImages]) != nil {
		t.Errorf("more than %d photos: %v", MaxImages, err)
	}
	if CheckImages(nil) != nil {
		t.Error("no photos is fine")
	}
}

func TestImageIDsAndNames(t *testing.T) {
	id := "up_" + strings.Repeat("f", 32)
	if !ValidImageID(id) || ImageName(id, ImageWebP) != id+".webp" {
		t.Errorf("a kept photo's id: %v %q", ValidImageID(id), ImageName(id, ImageWebP))
	}
	for _, bad := range []string{"", "up_", id + "0", id[:len(id)-1], strings.ToUpper(id), id + ".jpg", "../" + id, " " + id, id + "\n"} {
		if ValidImageID(bad) || ImageName(bad, ImageJPEG) != "" {
			t.Errorf("ValidImageID(%q) took it", bad)
		}
	}
	if ImageName(id, "image/gif") != "" {
		t.Error("a name for a type that is not taken")
	}
	// Every type has an ending and the endings differ.
	seen := map[string]bool{}
	for _, typ := range ImageTypes {
		ext := ImageExt[typ]
		if ext == "" || seen[ext] {
			t.Errorf("type %s has ending %q", typ, ext)
		}
		seen[ext] = true
	}
}
