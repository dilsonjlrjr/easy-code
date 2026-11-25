package gate

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

// Prototype annotations (US-57, PD-25 option a): pins with a comment that the approver of gate.design
// marks on the prototype and sends with the rejection; they stay in the round record and enter the
// rework wave of the Designer as untrusted input (RN-2). They never change TELAS (RN-4).
const (
	// AnnotationStage is the only stage whose rejection takes annotations.
	AnnotationStage = "design"
	// AnnotatedType is the artifact the annotations point at.
	AnnotatedType = "TELAS"
	// MaxAnnotations and MaxAnnotationComment bound one rejection.
	MaxAnnotations       = 50
	MaxAnnotationComment = 2000
	// Widths of the viewer when the pin was marked.
	WidthWide   = "wide"
	WidthNarrow = "narrow"
)

var (
	screenPattern = regexp.MustCompile(`^TL-[0-9]+$`)
	statePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Annotation is one pin: the screen, the state shown, the width and the point relative to the frame
// (0 to 1 on each axis; both nil for an annotation without a point, US-57 CA-5).
type Annotation struct {
	Number  int
	Screen  string
	State   string // "" when the screen was shown without ?estado=
	Width   string
	X, Y    *float64
	Comment string
	// Artifact and Version are the TELAS version the round decided.
	Artifact string
	Version  int
}

// AnnotationStore keeps the annotations of the gate rounds.
type AnnotationStore interface {
	// RecordAnnotations stores the annotations of a round, numbered from 1, by the person who rejected it.
	RecordAnnotations(ctx context.Context, principal identity.Principal, projectID, stage string, round int, artifact string, version int, annotations []Annotation) error
	// StageAnnotations returns the annotations of every round of the stage, by round number.
	StageAnnotations(ctx context.Context, projectID, stage string) (map[int][]Annotation, error)
}

// WithAnnotations enables the annotations of the gate.design rejection (US-57).
func (d *Decisions) WithAnnotations(store AnnotationStore) *Decisions {
	d.annotations = store
	return d
}

// validateAnnotations checks the annotations of a decision (openapi PrototypeAnnotation).
func validateAnnotations(stage string, approve bool, list []Annotation) error {
	if len(list) == 0 {
		return nil
	}
	if approve || stage != AnnotationStage {
		return failure.Invalid(failure.FieldError{Field: "annotations", Message: "invalid"})
	}
	if len(list) > MaxAnnotations {
		return failure.Invalid(failure.FieldError{Field: "annotations", Message: "too_many"})
	}
	var fields []failure.FieldError
	for i, a := range list {
		at := "annotations/" + strconv.Itoa(i) + "/"
		if !screenPattern.MatchString(a.Screen) {
			fields = append(fields, failure.FieldError{Field: at + "screen", Message: "pattern"})
		}
		if a.State != "" && !statePattern.MatchString(a.State) {
			fields = append(fields, failure.FieldError{Field: at + "state", Message: "pattern"})
		}
		if a.Width != WidthWide && a.Width != WidthNarrow {
			fields = append(fields, failure.FieldError{Field: at + "width", Message: "enum"})
		}
		if (a.X == nil) != (a.Y == nil) || !unit(a.X) || !unit(a.Y) {
			fields = append(fields, failure.FieldError{Field: at + "x", Message: "range"})
		}
		comment := strings.TrimSpace(a.Comment)
		if comment == "" {
			fields = append(fields, failure.FieldError{Field: at + "comment", Message: "required"})
		} else if utf8.RuneCountInString(a.Comment) > MaxAnnotationComment {
			fields = append(fields, failure.FieldError{Field: at + "comment", Message: "length"})
		}
	}
	if len(fields) > 0 {
		return failure.Invalid(fields...)
	}
	return nil
}

func unit(v *float64) bool { return v == nil || (*v >= 0 && *v <= 1) }

// annotatedVersion is the TELAS version among the outputs of the round.
func annotatedVersion(outputs []OutputRef) (string, int, bool) {
	for _, o := range outputs {
		if o.Type == AnnotatedType {
			return o.Code, o.Version, true
		}
	}
	return "", 0, false
}

// attachAnnotations puts the stored annotations of the stage into the round views (gate.design only).
func (d *Decisions) attachAnnotations(ctx context.Context, projectID, stage string, views []RoundView) error {
	if d.annotations == nil || stage != AnnotationStage || len(views) == 0 {
		return nil
	}
	byRound, err := d.annotations.StageAnnotations(ctx, projectID, stage)
	if err != nil {
		return err
	}
	for i := range views {
		views[i].Annotations = byRound[views[i].Round]
	}
	return nil
}

// RenderAnnotations writes the annotations for the prompt of the Designer rework wave (US-57 CA-2): one
// line per pin with the screen, the state, the width, the point and the comment as the approver wrote it.
// The caller keeps the text inside the untrusted input block (RN-2, RNF-05).
func RenderAnnotations(list []Annotation) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Anotações do aprovador no protótipo (%s versão %d):\n", list[0].Artifact, list[0].Version)
	for _, a := range list {
		state := a.State
		if state == "" {
			state = "padrão"
		}
		width := map[string]string{WidthWide: "larga", WidthNarrow: "estreita"}[a.Width]
		point := "sem ponto"
		if a.X != nil && a.Y != nil {
			point = fmt.Sprintf("ponto em %.0f%% da largura e %.0f%% da altura do quadro", *a.X*100, *a.Y*100)
		}
		// One line, and no tag that could close the untrusted block the text goes in.
		comment := strings.NewReplacer("<", "‹", ">", "›").Replace(strings.Join(strings.Fields(a.Comment), " "))
		fmt.Fprintf(&b, "%d. %s · estado %s · largura %s · %s: %s\n", a.Number, a.Screen, state, width, point, comment)
	}
	return b.String()
}
