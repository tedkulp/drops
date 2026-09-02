package resolve

import "strings"

// SlugCandidates returns the names an auto-created Project may take, in order:
// the bare repository name, then owner-name to disambiguate it. A numeric
// suffix is deliberately never a candidate — see AmbiguousSlug.
//
// locator must already be normalized. dirName is the base name of the
// repository root, used when there is no origin to name the repository.
func SlugCandidates(locator, dirName string) []string {
	if locator == "" {
		if slug := slugify(dirName); slug != "" {
			return []string{slug}
		}
		return nil
	}
	parts := strings.Split(locator, "/")
	name := slugify(parts[len(parts)-1])
	if name == "" {
		return nil
	}
	candidates := []string{name}
	if len(parts) >= 3 {
		if owner := slugify(parts[len(parts)-2]); owner != "" {
			candidates = append(candidates, owner+"-"+name)
		}
	}
	return candidates
}

// slugify reduces a name to the shape a slug is typed in: lowercase, with every
// run of other characters collapsed to a single dash. A slug is an argument to
// -P, so a space in it would have to be quoted at every call site.
func slugify(name string) string {
	var slug strings.Builder
	dashed := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '.', char == '_':
			slug.WriteRune(char)
			dashed = false
		default:
			if slug.Len() > 0 && !dashed {
				slug.WriteByte('-')
				dashed = true
			}
		}
	}
	return strings.Trim(slug.String(), "-.")
}
