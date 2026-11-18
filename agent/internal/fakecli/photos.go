package fakecli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/heeo-dev/askorus/agent/internal/uploads"
)

// Photos with a question (#14). Each fake takes them the way its AI does:
// claude as image blocks of the one stream-json message on stdin, codex as
// "-i <file>", opencode as "--file <file>". A photo that is not an image
// (by its first bytes), a file that is not there or a message that is not
// the stream's shape ends the run with exit 2, so a test sees a wrong path
// or a broken message at once. An ask that got photos says so at the end
// of its answer: " [photos: 2]".

// photoInput reads what the call got: how many photos, and the question's
// text (stdin, or for claude's stream input the message's text).
func photoInput(role string, args []string, stdin string) (photos int, prompt string, err error) {
	var files []string
	switch role {
	case RoleClaude:
		if flagValue(args, "--input-format") == "" {
			return 0, stdin, nil
		}
		return claudeStream(args, stdin)
	case RoleCodex:
		files = flagValues(args, "-i")
	case RoleOpenCode:
		files = flagValues(args, "--file")
	case RoleAgy:
		if flagValue(args, "--input-format") == "" {
			return 0, stdin, nil
		}
		return agyStream(args, stdin)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return 0, "", fmt.Errorf("photo: %w", err)
		}
		if uploads.Sniff(b) == "" {
			return 0, "", errors.New("photo: " + f + " is not an image")
		}
	}
	return len(files), stdin, nil
}

// claudeStream reads claude's stream-json input: one user message with a
// text block and image blocks, with the stream output and --verbose.
func claudeStream(args []string, stdin string) (int, string, error) {
	if flagValue(args, "--input-format") != "stream-json" || flagValue(args, "--output-format") != "stream-json" || !hasFlag(args, "--verbose") {
		return 0, "", errors.New("stream-json input needs stream-json output and --verbose")
	}
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source *struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(stdin), &m); err != nil || m.Type != "user" || m.Message.Role != "user" {
		return 0, "", errors.New("stdin is not one stream-json user message")
	}
	photos, prompt, texts := 0, "", 0
	for _, b := range m.Message.Content {
		switch b.Type {
		case "text":
			prompt = b.Text
			texts++
		case "image":
			if b.Source == nil || b.Source.Type != "base64" {
				return 0, "", errors.New("an image block without base64 data")
			}
			data, err := base64.StdEncoding.DecodeString(b.Source.Data)
			if err != nil || uploads.Sniff(data) != b.Source.MediaType {
				return 0, "", errors.New("an image block whose data is not an image of its media type")
			}
			photos++
		default:
			return 0, "", errors.New("unknown content block " + b.Type)
		}
	}
	if texts != 1 || photos == 0 {
		return 0, "", errors.New("the message needs one text block and at least one image")
	}
	return photos, prompt, nil
}

// photoNote is what an ask's answer ends with when it got photos.
func photoNote(c *Call) string {
	if c.Photos == 0 {
		return ""
	}
	return fmt.Sprintf(" [photos: %d]", c.Photos)
}

// flagValues returns every value of the flag name, in order.
func flagValues(args []string, name string) []string {
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}
