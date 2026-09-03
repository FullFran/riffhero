package gp

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// The types below mirror only the parts of a GPIF document RiffHero needs.
//
// Two habits run through them. First, every numeric attribute is read as a
// string and converted by hand: encoding/xml fails the whole document when a
// single attribute is not a number, and one broken id is not a reason to
// refuse a song. Second, elements whose absence means something (a rhythm with
// no dot, a note with no tie) are pointers, because the zero value of the
// element and "the element was not there" are different facts here.

type gpif struct {
	XMLName     xml.Name        `xml:"GPIF"`
	Score       scoreNode       `xml:"Score"`
	MasterTrack masterTrackNode `xml:"MasterTrack"`
	Tracks      []trackNode     `xml:"Tracks>Track"`
	MasterBars  []masterBarNode `xml:"MasterBars>MasterBar"`
	Bars        []barNode       `xml:"Bars>Bar"`
	Voices      []voiceNode     `xml:"Voices>Voice"`
	Beats       []beatNode      `xml:"Beats>Beat"`
	Notes       []noteNode      `xml:"Notes>Note"`
	Rhythms     []rhythmNode    `xml:"Rhythms>Rhythm"`
}

type scoreNode struct {
	Title    string `xml:"Title"`
	SubTitle string `xml:"SubTitle"`
	Artist   string `xml:"Artist"`
	Album    string `xml:"Album"`
	Music    string `xml:"Music"`
	Words    string `xml:"Words"`
}

type masterTrackNode struct {
	Tracks      string           `xml:"Tracks"`
	Automations []automationNode `xml:"Automations>Automation"`
}

type automationNode struct {
	Type     string `xml:"Type"`
	Bar      string `xml:"Bar"`
	Position string `xml:"Position"`
	Value    string `xml:"Value"`
	Linear   string `xml:"Linear"`
}

type trackNode struct {
	ID            string            `xml:"id,attr"`
	Name          string            `xml:"Name"`
	ShortName     string            `xml:"ShortName"`
	InstrumentSet instrumentSetNode `xml:"InstrumentSet"`
	Sounds        []soundNode       `xml:"Sounds>Sound"`
	Staves        []staffNode       `xml:"Staves>Staff"`
	// Some Guitar Pro 7 builds hang the tuning off the track instead of off a
	// staff, so both places are read.
	Properties []propertyNode `xml:"Properties>Property"`
}

type instrumentSetNode struct {
	Name string `xml:"Name"`
	Type string `xml:"Type"`
}

type soundNode struct {
	Name  string `xml:"Name"`
	Label string `xml:"Label"`
}

type staffNode struct {
	Properties []propertyNode `xml:"Properties>Property"`
}

// propertyNode is GPIF's generic key/value carrier. The name attribute says
// which of the child elements is the one with the value in it.
type propertyNode struct {
	Name    string `xml:"name,attr"`
	Pitches string `xml:"Pitches"`
	Fret    *int   `xml:"Fret"`
	String  *int   `xml:"String"`
	Number  *int   `xml:"Number"`
}

type masterBarNode struct {
	Time             string      `xml:"Time"`
	Bars             string      `xml:"Bars"`
	Repeat           *repeatNode `xml:"Repeat"`
	AlternateEndings string      `xml:"AlternateEndings"`

	// Da Capo / Dal Segno / Coda. These are read so the format is documented
	// here and so a file that uses them still parses, but they are never
	// followed: see expandRepeats.
	DirectionJump   []string       `xml:"DirectionJump"`
	DirectionTarget []string       `xml:"DirectionTarget"`
	Directions      directionsNode `xml:"Directions"`
}

type directionsNode struct {
	Jumps   []string `xml:"Jump"`
	Targets []string `xml:"Target"`
}

type repeatNode struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
	Count string `xml:"count,attr"`
}

type barNode struct {
	ID     string `xml:"id,attr"`
	Clef   string `xml:"Clef"`
	Voices string `xml:"Voices"`
}

type voiceNode struct {
	ID    string `xml:"id,attr"`
	Beats string `xml:"Beats"`
}

type beatNode struct {
	ID     string  `xml:"id,attr"`
	Rhythm refNode `xml:"Rhythm"`
	Notes  string  `xml:"Notes"`
	// A grace beat is an ornament that does not own any of the bar's time.
	// Its presence, not its value, is what matters to us.
	GraceNotes string `xml:"GraceNotes"`
}

type refNode struct {
	Ref string `xml:"ref,attr"`
}

type noteNode struct {
	ID         string         `xml:"id,attr"`
	Properties []propertyNode `xml:"Properties>Property"`
	Tie        *tieNode       `xml:"Tie"`
}

type tieNode struct {
	Origin      string `xml:"origin,attr"`
	Destination string `xml:"destination,attr"`
}

type rhythmNode struct {
	ID              string      `xml:"id,attr"`
	NoteValue       string      `xml:"NoteValue"`
	AugmentationDot *countNode  `xml:"AugmentationDot"`
	PrimaryTuplet   *tupletNode `xml:"PrimaryTuplet"`
}

type countNode struct {
	Count string `xml:"count,attr"`
}

type tupletNode struct {
	Num string `xml:"num,attr"`
	Den string `xml:"den,attr"`
}

// tables is the id -> element lookup the whole traversal runs on. GPIF joins
// its tables by id, and ids are not required to be dense or ordered, so an
// index is the only safe way to follow a reference.
type tables struct {
	bars    map[int]*barNode
	voices  map[int]*voiceNode
	beats   map[int]*beatNode
	notes   map[int]*noteNode
	rhythms map[int]*rhythmNode
}

func indexTables(doc *gpif) tables {
	t := tables{
		bars:    make(map[int]*barNode, len(doc.Bars)),
		voices:  make(map[int]*voiceNode, len(doc.Voices)),
		beats:   make(map[int]*beatNode, len(doc.Beats)),
		notes:   make(map[int]*noteNode, len(doc.Notes)),
		rhythms: make(map[int]*rhythmNode, len(doc.Rhythms)),
	}
	for i := range doc.Bars {
		if id, ok := parseInt(doc.Bars[i].ID); ok {
			t.bars[id] = &doc.Bars[i]
		}
	}
	for i := range doc.Voices {
		if id, ok := parseInt(doc.Voices[i].ID); ok {
			t.voices[id] = &doc.Voices[i]
		}
	}
	for i := range doc.Beats {
		if id, ok := parseInt(doc.Beats[i].ID); ok {
			t.beats[id] = &doc.Beats[i]
		}
	}
	for i := range doc.Notes {
		if id, ok := parseInt(doc.Notes[i].ID); ok {
			t.notes[id] = &doc.Notes[i]
		}
	}
	for i := range doc.Rhythms {
		if id, ok := parseInt(doc.Rhythms[i].ID); ok {
			t.rhythms[id] = &doc.Rhythms[i]
		}
	}
	return t
}

// fretPosition reads a note's tab position. The string number is returned as
// GPIF writes it, 0-based counting up from the lowest-sounding string; turning
// that into a RiffHero string number is practiceString's job and is done once,
// at the call site, so the inversion cannot happen twice by accident.
func (n *noteNode) fretPosition() (gpifString, fret int, ok bool) {
	haveString, haveFret := false, false
	for i := range n.Properties {
		p := &n.Properties[i]
		switch p.Name {
		case "String":
			if p.String != nil {
				gpifString, haveString = *p.String, true
			}
		case "Fret":
			if p.Fret != nil {
				fret, haveFret = *p.Fret, true
			}
		}
	}
	return gpifString, fret, haveString && haveFret
}

// tieDestination reports whether this note continues the previous note on the
// same string rather than being struck again.
func (n *noteNode) tieDestination() bool {
	return n.Tie != nil && parseBool(n.Tie.Destination)
}

func parseInt(s string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return v, true
}

func intOr(s string, def int) int {
	if v, ok := parseInt(s); ok {
		return v
	}
	return def
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// parseIntList reads GPIF's space-separated integer lists: bar ids per track,
// voice ids per bar, beat ids per voice, note ids per beat. Unparsable entries
// are dropped rather than failing the document.
func parseIntList(s string) []int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		if v, ok := parseInt(f); ok {
			out = append(out, v)
		}
	}
	return out
}
