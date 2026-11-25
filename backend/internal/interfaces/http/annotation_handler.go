package http

import appgate "easycode/internal/application/gate"

// annotationJSON is openapi PrototypeAnnotation as the rejection of gate.design sends it (US-57).
type annotationJSON struct {
	Screen  string   `json:"screen"`
	State   *string  `json:"state"`
	Width   string   `json:"width"`
	X       *float64 `json:"x"`
	Y       *float64 `json:"y"`
	Comment string   `json:"comment"`
}

// recordedAnnotationJSON is an annotation in the round record (GateRound.decision.annotations).
type recordedAnnotationJSON struct {
	Number   int      `json:"number"`
	Artifact string   `json:"artifact"`
	Version  int      `json:"version"`
	Screen   string   `json:"screen"`
	State    *string  `json:"state"`
	Width    string   `json:"width"`
	X        *float64 `json:"x"`
	Y        *float64 `json:"y"`
	Comment  string   `json:"comment"`
}

func annotationsJSON(list []appgate.Annotation) []recordedAnnotationJSON {
	out := make([]recordedAnnotationJSON, 0, len(list))
	for _, a := range list {
		var state *string
		if a.State != "" {
			state = &a.State
		}
		out = append(out, recordedAnnotationJSON{Number: a.Number, Artifact: a.Artifact, Version: a.Version, Screen: a.Screen, State: state,
			Width: a.Width, X: a.X, Y: a.Y, Comment: a.Comment})
	}
	return out
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
