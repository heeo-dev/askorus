package judge

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/asksemble/asksemble/agent/internal/core"
)

// validSchemaKeys reads the keys of every object of a JSON text in their
// order, by path ("" for the top, "/properties/judgments" and so on).
func validSchemaKeys(t *testing.T, text string) map[string][]string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	keys := map[string][]string{}
	var walk func(path string)
	walk = func(path string) {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		switch tok {
		case json.Delim('{'):
			keys[path] = []string{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				key := k.(string)
				if slices.Contains(keys[path], key) {
					t.Errorf("%s: key %q twice", path, key)
				}
				keys[path] = append(keys[path], key)
				walk(path + "/" + key)
			}
			dec.Token()
		case json.Delim('['):
			for i := 0; dec.More(); i++ {
				walk(path + "/" + strconv.Itoa(i))
			}
			dec.Token()
		}
	}
	walk("")
	return keys
}

// TestSchemaV1IsStrict checks core.JudgeSchemaV1 against what Codex's
// strict mode wants: every object schema lists all its properties as
// required and allows no others, every array has items, and no keyword is
// used that strict mode or Go would leave to the other side.
func TestSchemaV1IsStrict(t *testing.T) {
	var schema any
	if err := json.Unmarshal([]byte(core.JudgeSchemaV1), &schema); err != nil {
		t.Fatalf("the schema is not JSON: %v", err)
	}
	allowed := []string{"type", "properties", "items", "required", "additionalProperties", "enum"}
	objects := 0
	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
		for k := range node {
			if !slices.Contains(allowed, k) {
				t.Errorf("%s: keyword %q", path, k)
			}
		}
		switch node["type"] {
		case "object":
			objects++
			props, _ := node["properties"].(map[string]any)
			var names []string
			for name, p := range props {
				names = append(names, name)
				sub, ok := p.(map[string]any)
				if !ok {
					t.Fatalf("%s/%s is not a schema", path, name)
				}
				walk(path+"/"+name, sub)
			}
			var required []string
			list, _ := node["required"].([]any)
			for _, r := range list {
				required = append(required, r.(string))
			}
			sort.Strings(names)
			sort.Strings(required)
			if len(names) == 0 || !slices.Equal(names, required) {
				t.Errorf("%s: properties %v, required %v", path, names, required)
			}
			if node["additionalProperties"] != false {
				t.Errorf("%s: additionalProperties is %v", path, node["additionalProperties"])
			}
		case "array":
			items, ok := node["items"].(map[string]any)
			if !ok {
				t.Fatalf("%s: no items", path)
			}
			walk(path+"/items", items)
		case "string", "boolean":
		default:
			t.Errorf("%s: type %v", path, node["type"])
		}
	}
	walk("", schema.(map[string]any))
	if objects != 2 {
		t.Errorf("%d object schemas, want the top and one judgment", objects)
	}
}

// TestSchemaV1Order checks the order the judge fills a judgment in, which
// Go would lose by sorting map keys, and that the schema, the validator
// and the label constants agree.
func TestSchemaV1Order(t *testing.T) {
	keys := validSchemaKeys(t, core.JudgeSchemaV1)
	item := "/properties/judgments/items"
	if got := keys[item+"/properties"]; !slices.Equal(got, []string{"id", "core", "evidence", "reason", "label", "unsure"}) {
		t.Errorf("properties in order %v", got)
	}
	if !slices.Equal(keys[item+"/properties"], validFields) {
		t.Errorf("Validate reads %v, the schema has %v", validFields, keys[item+"/properties"])
	}
	if got := keys["/properties"]; !slices.Equal(got, []string{"judgments"}) {
		t.Errorf("top properties %v", got)
	}

	var schema struct {
		Properties struct {
			Judgments struct {
				Items struct {
					Properties struct {
						Label struct {
							Enum []string `json:"enum"`
						} `json:"label"`
					} `json:"properties"`
					Required []string `json:"required"`
				} `json:"items"`
			} `json:"judgments"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(core.JudgeSchemaV1), &schema); err != nil {
		t.Fatal(err)
	}
	it := schema.Properties.Judgments.Items
	if !slices.Equal(it.Required, validFields) {
		t.Errorf("required in order %v", it.Required)
	}
	if want := []string{LabelSame, LabelOpposite, LabelAbsent}; !slices.Equal(it.Properties.Label.Enum, want) || !slices.Equal(validLabels, want) {
		t.Errorf("labels: schema %v, Validate %v, want %v", it.Properties.Label.Enum, validLabels, want)
	}
	// The prompt's output example lists the fields in the same order.
	i := strings.Index(promptTail, `{"judgments":[{`)
	if i < 0 {
		t.Fatal("no output example in the prompt")
	}
	example := validSchemaKeys(t, strings.TrimSpace(promptTail[i:]))
	if got := example["/judgments/0"]; !slices.Equal(got, validFields) {
		t.Errorf("the prompt's example has fields %v", got)
	}
}
