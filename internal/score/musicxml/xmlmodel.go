package musicxml

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// scorePartwiseXML is the root of a MusicXML score-partwise document. The
// XMLName tag doubles as the format check: encoding/xml refuses to decode
// into this type if the root element is anything else, including
// score-timewise, so a bad root is reported before conversion ever starts.
type scorePartwiseXML struct {
	XMLName xml.Name `xml:"score-partwise"`
	Work    struct {
		Title string `xml:"work-title"`
	} `xml:"work"`
	MovementTitle  string `xml:"movement-title"`
	Identification struct {
		Creators []creatorXML `xml:"creator"`
	} `xml:"identification"`
	PartList struct {
		ScoreParts []scorePartXML `xml:"score-part"`
	} `xml:"part-list"`
	Parts []partXML `xml:"part"`
}

type creatorXML struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type scorePartXML struct {
	ID              string `xml:"id,attr"`
	Name            string `xml:"part-name"`
	ScoreInstrument struct {
		Name string `xml:"instrument-name"`
	} `xml:"score-instrument"`
}

type partXML struct {
	ID       string       `xml:"id,attr"`
	Measures []measureXML `xml:"measure"`
}

// measureItemKind tags what a measureItemXML carries. Backup and forward
// collapse to a single signed cursor move: the sign already says which one it
// was, and nothing downstream needs to tell them apart again.
type measureItemKind int

const (
	itemAttributes measureItemKind = iota
	itemNote
	itemCursorMove
	itemTempo
)

type measureItemXML struct {
	kind       measureItemKind
	attributes attributesXML
	note       noteXML
	cursorMove int64 // divisions; backup is encoded negative, forward positive
	tempoBPM   float64
}

// measureXML decodes its children by hand instead of letting encoding/xml
// bucket <attributes>, <note>, <backup> and <forward> into separate slices.
// The bucketed form throws away the one thing that makes a measure playable:
// the order those elements appear in. A <divisions> change only applies to
// what follows it, <chord/> only makes sense relative to the note right
// before it, and <backup>/<forward> only make sense as moves relative to
// wherever the cursor already is — all of that is lost the moment the
// elements are sorted by tag name instead of kept in document order.
type measureXML struct {
	items []measureItemXML

	// barlines are kept apart from items because, unlike everything in items,
	// they say nothing about where the cursor is: they describe the edges of
	// the measure as a whole, and they are read before conversion starts, to
	// work out the order the measures are played in.
	barlines []barlineXML
}

func (m *measureXML) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := m.decodeChild(d, t); err != nil {
				return err
			}
		case xml.EndElement:
			// Every child element above is fully consumed by DecodeElement or
			// Skip, which themselves swallow their own end tags. So the only
			// EndElement this loop ever sees directly is the one that closes
			// this <measure>.
			return nil
		}
	}
}

func (m *measureXML) decodeChild(d *xml.Decoder, t xml.StartElement) error {
	switch t.Name.Local {
	case "attributes":
		var a attributesXML
		if err := d.DecodeElement(&a, &t); err != nil {
			return err
		}
		m.items = append(m.items, measureItemXML{kind: itemAttributes, attributes: a})
	case "note":
		var n noteXML
		if err := d.DecodeElement(&n, &t); err != nil {
			return err
		}
		m.items = append(m.items, measureItemXML{kind: itemNote, note: n})
	case "backup":
		var b durationXML
		if err := d.DecodeElement(&b, &t); err != nil {
			return err
		}
		m.items = append(m.items, measureItemXML{kind: itemCursorMove, cursorMove: -b.Duration})
	case "forward":
		var f durationXML
		if err := d.DecodeElement(&f, &t); err != nil {
			return err
		}
		m.items = append(m.items, measureItemXML{kind: itemCursorMove, cursorMove: f.Duration})
	case "direction":
		var dir directionXML
		if err := d.DecodeElement(&dir, &t); err != nil {
			return err
		}
		if dir.Sound != nil {
			m.appendTempo(dir.Sound.Tempo)
		}
	case "sound":
		// <sound> can also appear directly under <measure> in older exports,
		// not only wrapped in <direction>.
		var s soundXML
		if err := d.DecodeElement(&s, &t); err != nil {
			return err
		}
		m.appendTempo(s.Tempo)
	case "barline":
		var b barlineXML
		if err := d.DecodeElement(&b, &t); err != nil {
			return err
		}
		m.barlines = append(m.barlines, b)
	default:
		if err := d.Skip(); err != nil {
			return err
		}
	}
	return nil
}

func (m *measureXML) appendTempo(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	bpm, err := strconv.ParseFloat(raw, 64)
	if err != nil || bpm <= 0 {
		return
	}
	m.items = append(m.items, measureItemXML{kind: itemTempo, tempoBPM: bpm})
}

type durationXML struct {
	Duration int64 `xml:"duration"`
}

type directionXML struct {
	Sound *soundXML `xml:"sound"`
}

type soundXML struct {
	Tempo string `xml:"tempo,attr"`
}

// barlineXML is one edge of a measure: the left edge when location is "left",
// the right edge otherwise (the attribute defaults to "right"). What matters
// here is the repeat structure hanging off it.
//
// BarStyle is decoded but nothing acts on it. It is the engraving of the line
// — "light-heavy" for a final barline, "heavy-light" beside a forward repeat —
// and a practice timeline has no engraving; it is kept because reading a
// barline without reading its style makes the next person wonder whether it
// was overlooked or deliberately ignored.
type barlineXML struct {
	Location string     `xml:"location,attr"`
	BarStyle string     `xml:"bar-style"`
	Repeat   *repeatXML `xml:"repeat"`
	Ending   *endingXML `xml:"ending"`
}

// repeatXML is a repeat sign. Direction is "forward" (the section starts here)
// or "backward" (jump back to the last forward sign); Times is how many times
// the section is played in total and is usually absent, meaning twice.
type repeatXML struct {
	Direction string `xml:"direction,attr"`
	Times     string `xml:"times,attr"`
}

// endingXML is a numbered alternate ending bracket. Number is a comma
// separated list of the passes the bracket belongs to ("1", or "1, 2"), and
// Type is "start" on the bracket's opening barline and "stop" or
// "discontinue" on its closing one.
type endingXML struct {
	Number string `xml:"number,attr"`
	Type   string `xml:"type,attr"`
}

type attributesXML struct {
	Divisions    *float64          `xml:"divisions"`
	Time         *timeXML          `xml:"time"`
	StaffDetails []staffDetailsXML `xml:"staff-details"`
}

type timeXML struct {
	Beats    string `xml:"beats"`
	BeatType string `xml:"beat-type"`
}

type staffDetailsXML struct {
	StaffLines   *int             `xml:"staff-lines"`
	StaffTunings []staffTuningXML `xml:"staff-tuning"`
}

type staffTuningXML struct {
	Line         int      `xml:"line,attr"`
	TuningStep   string   `xml:"tuning-step"`
	TuningAlter  *float64 `xml:"tuning-alter"`
	TuningOctave int      `xml:"tuning-octave"`
}

type noteXML struct {
	Pitch     *pitchXML     `xml:"pitch"`
	Rest      *struct{}     `xml:"rest"`
	Duration  *int64        `xml:"duration"`
	Voice     string        `xml:"voice"`
	Chord     *struct{}     `xml:"chord"`
	Grace     *struct{}     `xml:"grace"`
	Ties      []tieXML      `xml:"tie"`
	Notations *notationsXML `xml:"notations"`
}

type pitchXML struct {
	Step   string   `xml:"step"`
	Alter  *float64 `xml:"alter"`
	Octave int      `xml:"octave"`
}

type tieXML struct {
	Type string `xml:"type,attr"`
}

type notationsXML struct {
	Technical *technicalXML `xml:"technical"`
}

// StringNum/FretNum are named apart from Go's string/fret vocabulary
// (practice.Note.String, .Fret) only to keep this file's field list from
// reading like a type pun.
type technicalXML struct {
	StringNum *int `xml:"string"`
	FretNum   *int `xml:"fret"`
}

type containerXML struct {
	Rootfiles struct {
		Rootfile []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}
