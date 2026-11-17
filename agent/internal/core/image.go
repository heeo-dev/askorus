package core

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// Photos with a question (#14, 2026-11-18). The phone uploads each photo
// to the agent first (internal/uploads), which keeps it under a name of its
// own; an ask or a combined answer then names the kept files. An adapter
// hands them to its AI in that AI's own way: Claude gets the bytes on stdin
// with the prompt, Codex and OpenCode get the path as an argument. The path
// is the agent's own (never the phone's file name), and CheckImage is what
// every adapter runs before a path goes near a command line.

// Image is one kept photo.
type Image struct {
	Path string // absolute, as internal/uploads made it
	Type string // ImageJPEG, ImagePNG or ImageWebP
}

// The photo types an ask takes.
const (
	ImageJPEG = "image/jpeg"
	ImagePNG  = "image/png"
	ImageWebP = "image/webp"
)

// ImageTypes lists them, as /v1/info shows them.
var ImageTypes = []string{ImageJPEG, ImagePNG, ImageWebP}

// ImageExt is the file ending internal/uploads gives a photo of each type.
var ImageExt = map[string]string{ImageJPEG: ".jpg", ImagePNG: ".png", ImageWebP: ".webp"}

const (
	// MaxImages is how many photos one question may carry.
	MaxImages = 4
	// MaxImageBytes bounds one photo. The phone sends them scaled down
	// (long edge 2048 px), usually well under 1 MiB.
	MaxImageBytes = 4 << 20
)

// ErrCodeNoImages: the question came with photos and this AI's model does
// not see photos (ImageSeer). The AI is not asked.
const ErrCodeNoImages = "no_image_support"

// imageNameRe is the name internal/uploads gives a kept photo: "up_", 32
// hex digits and the ending of its type. Nothing of it comes from the phone.
var imageNameRe = regexp.MustCompile(`^up_[0-9a-f]{32}\.(jpg|png|webp)$`)

// imageIDRe is a kept photo's id: its name without the ending.
var imageIDRe = regexp.MustCompile(`^up_[0-9a-f]{32}$`)

// ValidImageID reports whether id looks like a kept photo's id.
func ValidImageID(id string) bool { return imageIDRe.MatchString(id) }

// ImageName is the file name of the kept photo id of type typ, or "" when
// either is not valid.
func ImageName(id, typ string) string {
	ext, ok := ImageExt[typ]
	if !ok || !ValidImageID(id) {
		return ""
	}
	return id + ext
}

// CheckImage accepts a photo as internal/uploads keeps one: an absolute,
// clean path whose last part is a kept photo's name with the ending of its
// type. It does not look at the disk.
func CheckImage(im Image) error {
	ext, ok := ImageExt[im.Type]
	switch {
	case !ok:
		return fmt.Errorf("image type %q refused", im.Type)
	case !filepath.IsAbs(im.Path) || filepath.Clean(im.Path) != im.Path:
		return fmt.Errorf("image path refused: not absolute and clean")
	case !imageNameRe.MatchString(filepath.Base(im.Path)) || filepath.Ext(im.Path) != ext:
		return fmt.Errorf("image path refused: not a kept photo of type %s", im.Type)
	}
	return nil
}

// CheckImages accepts at most MaxImages photos, each one CheckImage
// accepts, none twice.
func CheckImages(ims []Image) error {
	if len(ims) > MaxImages {
		return fmt.Errorf("%d images refused: at most %d", len(ims), MaxImages)
	}
	seen := map[string]bool{}
	for _, im := range ims {
		if err := CheckImage(im); err != nil {
			return err
		}
		if seen[im.Path] {
			return fmt.Errorf("an image is named twice")
		}
		seen[im.Path] = true
	}
	return nil
}

// ImageSeer is implemented by askers that know which of their models see
// photos. An asker that is no ImageSeer takes no photos.
type ImageSeer interface {
	// SeesImages reports whether model, as an ask names it, sees photos.
	SeesImages(model string) bool
	// ImageModels is what /v1/info shows: All, or the models that see.
	ImageModels() ImageInfo
}

// ImageInfo tells the app which models of an AI see photos (AskInfo.images):
// all of them, or only the ones listed (a model's name without its
// "#variant"). The app leaves an AI out of a question with photos when its
// model is not among them, and says so before sending.
type ImageInfo struct {
	All    bool     `json:"all,omitempty"`
	Models []string `json:"models,omitempty"`
}
