package specs

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// YAMLToJSON converts one YAML document to JSON. Scalars keep the YAML core types except
// timestamps, which stay strings (like the loader of specs/dominio/ferramentas: 2026-11-19 without
// quotes is text, so schemas with "format: date" see the same value).
func YAMLToJSON(source []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return []byte("null"), nil
	}
	value, err := convert(document.Content[0])
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func convert(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return convert(node.Content[0])
	case yaml.AliasNode:
		return convert(node.Alias)
	case yaml.MappingNode:
		object := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: only text keys are supported", key.Line)
			}
			value, err := convert(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			object[key.Value] = value
		}
		return object, nil
	case yaml.SequenceNode:
		list := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := convert(item)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		return list, nil
	default:
		return scalar(node)
	}
}

func scalar(node *yaml.Node) (any, error) {
	switch node.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		return strconv.ParseBool(node.Value)
	case "!!int":
		if n, err := strconv.ParseInt(node.Value, 0, 64); err == nil {
			return n, nil
		}
		return node.Value, nil
	case "!!float":
		if f, err := strconv.ParseFloat(node.Value, 64); err == nil {
			return f, nil
		}
		return node.Value, nil
	default: // !!str, !!timestamp and anything else stay text
		return node.Value, nil
	}
}
