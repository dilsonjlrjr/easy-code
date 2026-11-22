package specs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	domainartifact "easycode/internal/domain/artifact"
)

// ArtifactValidator validates artifact headers against the embedded schemas of
// specs/dominio/artefatos (draft 2020-12, formats asserted). The relative reference to the common
// header ("cabecalho-comum.schema.json") resolves through the local registry, without network,
// as in specs/dominio/ferramentas.
type ArtifactValidator struct {
	schemas map[string]*jsonschema.Schema // by type
}

// NewArtifactValidator compiles every schema once.
func NewArtifactValidator() (*ArtifactValidator, error) {
	entries, err := fs.ReadDir(files, "data/artefatos")
	if err != nil {
		return nil, err
	}
	documents := map[string]map[string]any{}
	ids := map[string]string{}
	for _, entry := range entries {
		raw, err := files.ReadFile("data/artefatos/" + entry.Name())
		if err != nil {
			return nil, err
		}
		decoded, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		document, ok := decoded.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: not an object", entry.Name())
		}
		id, _ := document["$id"].(string)
		if id == "" {
			return nil, fmt.Errorf("%s: $id is required", entry.Name())
		}
		documents[entry.Name()] = document
		ids[entry.Name()] = id
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for name, document := range documents {
		rewriteRefs(document, ids)
		if err := compiler.AddResource(ids[name], document); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	validator := &ArtifactValidator{schemas: map[string]*jsonschema.Schema{}}
	for _, artifactType := range domainartifact.Types {
		name := domainartifact.SchemaFile(artifactType)
		if _, ok := ids[name]; !ok {
			return nil, fmt.Errorf("no schema for type %s (%s)", artifactType, name)
		}
		schema, err := compiler.Compile(ids[name])
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", name, err)
		}
		validator.schemas[artifactType] = schema
	}
	return validator, nil
}

// rewriteRefs points references to sibling files at their $id.
func rewriteRefs(node any, ids map[string]string) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if ref, ok := child.(string); ok && key == "$ref" {
				file, fragment, _ := strings.Cut(ref, "#")
				if id, known := ids[path.Base(file)]; known && file != "" {
					value[key] = id + suffix(fragment)
				}
				continue
			}
			rewriteRefs(child, ids)
		}
	case []any:
		for _, child := range value {
			rewriteRefs(child, ids)
		}
	}
}

func suffix(fragment string) string {
	if fragment == "" {
		return ""
	}
	return "#" + fragment
}

// Validate implements artifact.Validator.
func (v *ArtifactValidator) Validate(artifactType string, header map[string]any) ([]domainartifact.SchemaError, error) {
	schema, ok := v.schemas[artifactType]
	if !ok {
		return []domainartifact.SchemaError{{Path: "/tipo", Message: fmt.Sprintf("unknown artifact type %q", artifactType)}}, nil
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	err = schema.Validate(instance)
	if err == nil {
		return nil, nil
	}
	var invalid *jsonschema.ValidationError
	if !errors.As(err, &invalid) {
		return nil, err
	}
	return schemaErrors(invalid), nil
}

var printer = message.NewPrinter(language.English)

// schemaErrors flattens the leaves of the validation tree, one per location and message.
func schemaErrors(root *jsonschema.ValidationError) []domainartifact.SchemaError {
	seen := map[string]bool{}
	var out []domainartifact.SchemaError
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				walk(cause)
			}
			return
		}
		location := "/" + strings.Join(e.InstanceLocation, "/")
		if location == "/" {
			location = ""
		}
		message := e.ErrorKind.LocalizedString(printer)
		if key := location + "\x00" + message; !seen[key] {
			seen[key] = true
			out = append(out, domainartifact.SchemaError{Path: location, Message: message})
		}
	}
	walk(root)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// HeaderParser parses the YAML front matter of artifacts.
type HeaderParser struct{}

// ParseHeader implements artifact.HeaderParser: the header must be a YAML mapping.
func (HeaderParser) ParseHeader(text string) (map[string]any, error) {
	encoded, err := YAMLToJSON([]byte(text))
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var header map[string]any
	if err := decoder.Decode(&header); err != nil || header == nil {
		return nil, errors.New("header must be a YAML mapping")
	}
	return header, nil
}
