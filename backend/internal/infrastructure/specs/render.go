package specs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// headerOrder is the order of the common header fields (cabecalho-comum.schema.json); the other fields
// follow in alphabetical order.
var headerOrder = []string{"id", "tipo", "titulo", "autor", "onda", "versao", "estado", "vinculos", "historico"}

// ArtifactRenderer writes an artifact file written by the platform itself (the RELEASE of the orchestrator):
// YAML front matter between "---" lines and the Markdown body. It implements gate.Renderer.
type ArtifactRenderer struct{}

// Render implements gate.Renderer.
func (ArtifactRenderer) Render(header map[string]any, body string) ([]byte, error) {
	node, err := valueNode(header, true)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return []byte("---\n" + out.String() + "---\n\n" + strings.TrimLeft(body, "\n")), nil
}

func valueNode(value any, top bool) (*yaml.Node, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if top {
			sort.SliceStable(keys, func(i, j int) bool { return rank(keys[i]) < rank(keys[j]) })
		}
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range keys {
			child, err := valueNode(v[k], false)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, child)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range v {
			child, err := valueNode(item, false)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case []string:
		items := make([]any, 0, len(v))
		for _, s := range v {
			items = append(items, s)
		}
		return valueNode(items, false)
	case string:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		var parsed any
		err := yaml.Unmarshal([]byte(v), &parsed)
		if same, ok := parsed.(string); err != nil || !ok || same != v || strings.ContainsAny(v, "\n#") {
			node.Style = yaml.DoubleQuotedStyle // a plain scalar would read back as another value (date, number, map…)
		}
		return node, nil
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: v.String()}, nil
	default:
		node := &yaml.Node{}
		if err := node.Encode(v); err != nil {
			return nil, fmt.Errorf("render header value: %w", err)
		}
		return node, nil
	}
}

func rank(key string) int {
	if i := slices.Index(headerOrder, key); i >= 0 {
		return i
	}
	return len(headerOrder)
}
