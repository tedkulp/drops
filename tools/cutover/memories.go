package main

import "github.com/tedkulp/drops/internal/model"

// The legacy notebook's curation, decided by dw32p.11 against the 149 memories
// the authoritative v6 store held on 2026-09-02: 118 retained under a
// semantically chosen owner, 31 dropped, and eight supersession edges preserved
// because live records reference the retired IDs.
//
// Ownership here is semantic, not historical: `global` keeps only cross-project
// knowledge, and a subject-specific row that happened to be filed globally moves
// to the Project it describes. Two owners — `ansible` and `skipper` — name
// Projects the legacy store never had; the conversion creates them because
// retained memories need the namespace.
//
// A memory absent from both tables is retained under its legacy scope. See
// Curate: the tables are a measurement of one store at one moment, not a
// closed world.

// retainedMemories maps a retained memory ID to the Project slug that owns it.
var retainedMemories = map[model.ID]string{}

// droppedMemories is the set of memory IDs the curation omits. They do not
// become v7 tombstones: this is a one-time migration judgement, not a deletion
// anybody performed.
var droppedMemories = map[model.ID]struct{}{}

// retainedSupersessions are the eight legacy supersession edges that survive,
// each from a retired record to the record that replaced it. They survive
// because issues and comments cite the retired IDs, so resolving one has to
// still lead somewhere.
var retainedSupersessions = map[model.ID]model.ID{
	"br-eyp":      "br-et8",
	"br-gd8":      "br-hor6",
	"br-n2in":     "br-hor6",
	"br-o1tv":     "br-fslg",
	"br-pep":      "br-hor6",
	"br-xo1n":     "br-gu78",
	"br-ytr1":     "br-da4q",
	"beacon-4psr": "beacon-lfn1",
}

func init() {
	for slug, ids := range retainedByProject {
		for _, id := range ids {
			retainedMemories[id] = slug
		}
	}
	for _, id := range dropped {
		droppedMemories[id] = struct{}{}
	}
}

// retainedByProject is the curation as dw32p.11 recorded it, grouped by owner
// so the count per Project stays readable against the ticket.
var retainedByProject = map[string][]model.ID{
	"global": {
		"br-9nf", "br-c4s", "br-s1f", "br-4jy", "br-04p8", "br-36k2", "br-qeuo",
		"br-esel", "br-8925", "br-f4pu", "br-hor6", "br-gu78", "br-fslg", "br-da4q",
		"br-x6p2", "br-kjy4", "br-gv3l", "br-q0zv", "br-t2qq", "br-iz1d", "br-z8di",
		"br-nuxs", "br-42t0", "br-py9i", "br-jigl", "br-gd8", "br-n2in", "br-o1tv",
		"br-pep", "br-xo1n", "br-ytr1",
	},
	"drops": {
		"br-et8", "br-1be", "br-sgs", "br-c47", "br-8i7", "br-ogg", "br-tvq",
		"br-r4q", "br-2r0", "br-ihs", "br-wtb7", "br-6amu", "br-sv3l", "br-xokd",
		"br-eyp",
	},
	"beacon": {
		"beacon-1b0", "beacon-xys", "beacon-z3o", "beacon-jk02", "beacon-4syp",
		"beacon-zsg9", "beacon-h8my", "beacon-tsqb", "beacon-5x7g", "beacon-94hi",
		"beacon-dw94", "beacon-01s4", "beacon-hxa6", "beacon-knwp", "beacon-vzdg",
		"beacon-rr8d", "beacon-uos1", "beacon-7cbh", "beacon-iuky", "beacon-z5b3",
		"beacon-no5d", "beacon-s9om", "beacon-lfn1", "beacon-8vx4", "beacon-4psr",
		"beacon-9ll", "beacon-bm9e", "beacon-j4nd",
	},
	"gitops": {
		"gitops-rop", "gitops-7mz", "gitops-d1v", "gitops-ovv", "gitops-naq",
		"gitops-ytv", "gitops-r1w", "gitops-60s", "gitops-fg3", "gitops-b2z",
		"gitops-2bj", "gitops-6an", "gitops-ish", "gitops-4t4", "gitops-o3k",
		"gitops-cja", "gitops-rhj", "gitops-jbs", "gitops-4f3", "gitops-gdn",
		"gitops-u84", "gitops-lqc", "gitops-7fa", "gitops-gm0", "gitops-uy8",
		"gitops-50k", "gitops-wbk",
	},
	"ansible":                    {"br-ksvc", "br-xz7j", "br-80u0", "br-sqxh", "br-8gc2"},
	"skipper":                    {"br-j835", "br-zzce", "br-4cvn", "br-eh9y"},
	"bd-shim":                    {"bd-shim-kf7", "bd-shim-kct", "bd-shim-m1v"},
	"gitlab-backup":              {"gitlab-backup-59l"},
	"mindthegap":                 {"mindthegap-937", "mindthegap-vu5"},
	"persistentpin-webextension": {"br-797"},
	"tix":                        {"br-ynwf"},
}

// dropped is the 31 omitted memories, in the three groups the curation gave:
// unreferenced superseded or duplicate records, obsolete implementation and
// status snapshots, and residue from dead workstations or retired harnesses.
var dropped = []model.ID{
	"br-0e76", "br-8lh", "br-fvd", "br-iga", "br-o5w", "br-wjh", "br-all",

	"br-dnb", "br-cdsj", "br-3kkv", "beacon-lca", "beacon-8bv", "beacon-9hn",
	"beacon-5sa", "beacon-veu", "beacon-tiu", "beacon-1kt", "beacon-mam",
	"beacon-qmy", "beacon-cla", "beacon-stb", "beacon-8js", "beacon-dorj",
	"beacon-sjsf", "beacon-2ev1", "gitops-mf1", "gitops-rw1",

	"br-yfot", "br-tj2m", "br-ai7u", "br-oa1g",
}
