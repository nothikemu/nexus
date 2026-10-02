package config

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nothikemu/nexus/internal/textutil"
)

// unknownKeys walks the YAML document against the Config struct and reports
// keys Nexus doesn't recognise, with a suggestion when one is close.
func unknownKeys(root *yaml.Node) []string {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	var out []string
	walkKeys(root.Content[0], reflect.TypeOf(Config{}), "", &out)
	return out
}

func walkKeys(n *yaml.Node, t reflect.Type, path string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			fields[name] = f.Type
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			full := join(path, key)
			ft, ok := fields[key]
			if !ok {
				msg := fmt.Sprintf("line %d: unknown key %q", n.Content[i].Line, full)
				if s := textutil.Closest(key, keysOf(fields), 2); s != "" {
					msg += fmt.Sprintf(" (did you mean %q?)", join(path, s))
				}
				*out = append(*out, msg)
				continue
			}
			walkKeys(n.Content[i+1], ft, full, out)
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			walkKeys(n.Content[i+1], t.Elem(), join(path, n.Content[i].Value), out)
		}
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func keysOf(m map[string]reflect.Type) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
