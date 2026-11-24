package specs

import (
	"errors"

	"easycode/internal/application/install"
	"easycode/internal/domain/skill"
)

// InstallCatalog implements install.SkillCatalog with the embedded native catalog: before the platform
// exists the worker has not published it yet, so the native names come from the binary.
type InstallCatalog struct{}

var _ install.SkillCatalog = InstallCatalog{}

// Native implements install.SkillCatalog.
func (InstallCatalog) Native() ([]install.SkillSummary, error) {
	packages, err := NativeSkills()
	if err != nil {
		return nil, err
	}
	out := make([]install.SkillSummary, len(packages))
	for i, p := range packages {
		out[i] = install.SkillSummary{Name: p.Name, Description: p.Description}
	}
	return out, nil
}

// Check implements install.SkillCatalog: the checks of the import (US-44 CA-2, CA-3) and a native name.
func (c InstallCatalog) Check(pkg install.SkillPackage) (install.SkillSummary, error) {
	if len(pkg.Content) > skill.MaxPackageBytes {
		return install.SkillSummary{}, install.Invalids{{Field: "skills", Reason: "too_large"}}
	}
	parsed, _, err := skill.ParseArchive(pkg.Content, YAMLToJSON)
	var invalid skill.Invalid
	if errors.As(err, &invalid) {
		return install.SkillSummary{}, install.Invalids{{Field: "skills", Reason: invalid.Reason}}
	}
	if err != nil {
		return install.SkillSummary{}, err
	}
	native, err := c.Native()
	if err != nil {
		return install.SkillSummary{}, err
	}
	for _, n := range native {
		if n.Name == parsed.Name {
			return install.SkillSummary{}, install.Invalids{{Field: "skills", Reason: "name_of_native_skill"}}
		}
	}
	return install.SkillSummary{Name: parsed.Name, Description: parsed.Description}, nil
}
