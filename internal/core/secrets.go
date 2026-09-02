package core

import (
	"regexp"

	"github.com/tedkulp/drops/internal/model"
)

type secretPattern struct {
	family string
	re     *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"AWS access key id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"GitHub personal access token", regexp.MustCompile(`\bghp_[0-9A-Za-z]{36}\b`)},
	{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[0-9A-Za-z_]{22,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}`)},
	{"API key (sk- family)", regexp.MustCompile(`\bsk-(ant-)?[0-9A-Za-z_-]{20,}`)},
}

func matchedSecrets(text string) []string {
	var families []string
	for _, pattern := range secretPatterns {
		if pattern.re.MatchString(text) {
			families = append(families, pattern.family)
		}
	}
	return families
}

type textField struct {
	name string
	text string
}

func (core *Core) emitWarnings(kind model.RecordKind, id string, fields []textField) {
	if core.warn == nil {
		return
	}
	core.warnMu.Lock()
	defer core.warnMu.Unlock()
	for _, field := range fields {
		for _, family := range matchedSecrets(field.text) {
			core.warn(Warning{
				Entity: model.RecordRef{Kind: kind, Key: id},
				Field:  field.name,
				Family: family,
			})
		}
	}
}
