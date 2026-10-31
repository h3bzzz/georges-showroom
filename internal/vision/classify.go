package vision

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// Class is a zero-shot label: a name and the prompts whose mean text embedding stands for it.
type Class struct {
	Name    string
	Prompts []string
}

// Medium classes: what kind of picture the crop is. "spec" is a photo or render with
// dimension lines; crop_kind() in SQL folds it into drawing. "other" catches portraits,
// swatches and blurry fragments.
var MediumClasses = buildMedium()

// Subject classes: what the picture shows. A family's category decides which subjects may
// illustrate it (subject_fits() in SQL): a handle never stands for a faucet, a drain never
// stands for a sink. Labels may use the finer names in SubjectMerge.
var SubjectClasses = buildSubject()

// SubjectMerge folds label names that the model cannot keep apart (and that the category
// check treats alike) into one class.
var SubjectMerge = map[string]string{"valve": "part", "cabinet": "hardware", "doorware": "hardware"}

// MergeSubject maps a label to its class name.
func MergeSubject(s string) string {
	if m, ok := SubjectMerge[s]; ok {
		return m
	}
	return s
}

var mediumThings = []string{"faucet", "sink", "bathtub", "shower head", "door lever", "cabinet knob", "plumbing fixture", "valve"}

func expand(templates []string, things []string, fixed ...string) []string {
	var out []string
	for _, t := range templates {
		for _, th := range things {
			out = append(out, fmt.Sprintf(t, th))
		}
	}
	return append(out, fixed...)
}

func buildMedium() []Class {
	return []Class{
		{Name: "photo", Prompts: expand([]string{
			"a product photo of a %s", "a photograph of a %s on a white background",
			"a colour photo of a polished %s", "a catalog photo of a whole %s", "a photo of a %s in a bathroom",
		}, mediumThings)},
		{Name: "detail", Prompts: expand([]string{
			"a close-up photo of part of a %s", "a cropped photo showing only the handle of a %s",
			"a macro photo of the surface of a %s", "a zoomed-in photo of a fragment of a %s",
		}, mediumThings, "an extreme close-up of a knurled metal texture", "a tightly cropped photo that cuts the object off at the edge")},
		{Name: "drawing", Prompts: expand([]string{
			"a black and white line drawing of a %s", "a technical drawing of a %s",
			"an outline sketch of a %s", "an engineering diagram of a %s", "a CAD line drawing of a %s",
		}, mediumThings, "an exploded parts diagram", "a dimensioned drawing with measurement arrows", "a schematic with dashed lines")},
		{Name: "spec", Prompts: expand([]string{
			"a photo of a %s with dimension lines and measurements in inches",
			"a product photo of a %s annotated with sizes and arrows",
			"a grey 3D rendering of a %s with dimension arrows",
		}, mediumThings)},
		{Name: "text", Prompts: []string{
			"a table of part numbers and prices", "a price list", "a page of printed text", "a company logo",
			"a QR code", "a text label on a coloured background", "a single large letter", "a spreadsheet",
			"a certification badge", "a column of numbers",
		}},
		{Name: "other", Prompts: []string{
			"a portrait photo of a person", "a blurry unrecognisable image", "a blank white image",
			"a photo of a tile or stone sample", "a photo of a room with no products", "a black rectangle",
		}},
	}
}

func buildSubject() []Class {
	both := func(things ...string) []string {
		return expand([]string{"a photo of %s", "a drawing of %s"}, things)
	}
	return []Class{
		{Name: "faucet", Prompts: both("a faucet", "a kitchen faucet with a tall spout", "a bathroom sink faucet with two handles",
			"a wall-mounted spout", "a bridge faucet", "a freestanding tub filler", "a pot filler on an articulated arm", "a widespread lavatory faucet")},
		{Name: "handle", Prompts: both("a single faucet handle by itself", "a lever handle on a small round base", "a cross handle",
			"a knob handle for a faucet", "a tap handle with no spout")},
		{Name: "trim", Prompts: both("a round shower valve trim plate with a lever", "a thermostatic shower control on a wall plate",
			"a square shower mixer trim", "a volume control handle on an escutcheon")},
		{Name: "shower", Prompts: both("a shower head", "a hand shower on a hose", "a shower slide bar with a handshower",
			"a rain shower head", "a shower arm", "a shower column", "a body spray jet")},
		{Name: "sink", Prompts: both("a bathroom sink", "a kitchen sink", "a vessel basin", "a bathroom vanity with a sink",
			"a pedestal sink", "a farmhouse sink")},
		{Name: "tub", Prompts: both("a bathtub", "a freestanding bathtub", "a clawfoot tub")},
		{Name: "toilet", Prompts: both("a toilet", "a bidet", "a urinal", "a toilet seat")},
		{Name: "part", Prompts: both("a faucet cartridge", "a flexible braided hose", "a drain assembly", "a metal escutcheon ring",
			"a small replacement part", "a power supply with a cord", "a sink bottom grid", "a p-trap", "an aerator", "a drain grate",
			"a brass rough-in valve body", "a plumbing valve with pipe connections", "a water filter cartridge")},
		{Name: "accessory", Prompts: both("a towel bar", "a towel warmer rack", "a toilet paper holder", "a soap dispenser pump",
			"a robe hook", "a grab bar", "a towel ring", "a mirror")},
		{Name: "hardware", Prompts: both("a cabinet knob", "a cabinet pull handle", "a drawer pull", "a cup pull", "several cabinet pulls in different finishes",
			"a door lever on a round rose", "a door knob", "a door handleset with a long plate", "a deadbolt",
			"a door hinge", "a strike plate", "a door knocker", "a door lever on a square rose")},
		{Name: "appliance", Prompts: both("a range hood", "a cooktop", "an oven", "a refrigerator", "a dishwasher", "a kitchen range")},
		// Buster + Punch sells lighting and switch plates beside its hardware; without these two
		// classes every pendant and socket read as "other" and was refused for every family
		{Name: "lighting", Prompts: both("a pendant lamp hanging from a cord", "a wall sconce", "a ceiling light", "a decorative light bulb",
			"a chandelier", "a table lamp", "three pendant lights in a row", "a brass pendant light with a glass globe")},
		{Name: "electrical", Prompts: both("a light switch plate", "an electrical socket", "a dimmer switch on a metal plate", "a toggle switch",
			"a wall plate with USB sockets")},
		{Name: "other", Prompts: []string{"a photo of a person", "a logo", "a tile sample", "an abstract shape", "printed text", "a blank image",
			"a t-shirt", "a hoodie", "a bottle of soap", "a necklace", "a wood sample", "a mood board of material samples"}},
	}
}

// Head scores an image embedding against classes. Text prototypes are the mean prompt
// embedding per class (zero-shot); image prototypes are the mean embedding of hand-labelled
// crops per class (few-shot). The two heads' softmax distributions are mixed, which is scale
// free: image–image cosines run far higher than image–text cosines.
type Head struct {
	Names     []string
	Text      [][]float32
	Proto     [][]float32 // nil entries fall back to the text prototype
	Temp      float32     // CLIP logit scale for text (100)
	ProtoTemp float32     // logit scale for image prototypes
	Mix       float32     // weight of the prototype distribution, 0 = zero-shot only
	// Probe, when set, is mixed in with ProbeWeight (applied after the prototype mix)
	Probe       *Probe
	ProbeWeight float32
}

// NewHead embeds the prompts of each class.
func (e *Encoder) NewHead(ctx context.Context, classes []Class) (*Head, error) {
	h := &Head{Temp: 100, ProtoTemp: 20, Mix: 0}
	for _, c := range classes {
		embs, err := e.Texts(ctx, c.Prompts)
		if err != nil {
			return nil, fmt.Errorf("class %s: %w", c.Name, err)
		}
		h.Names = append(h.Names, c.Name)
		h.Text = append(h.Text, Mean(embs))
		h.Proto = append(h.Proto, nil)
	}
	return h, nil
}

// SetPrototypes installs per-class image prototypes from labelled embeddings (class → vectors).
func (h *Head) SetPrototypes(byClass map[string][][]float32, mix float32) {
	for i, n := range h.Names {
		h.Proto[i] = Mean(byClass[n])
	}
	h.Mix = mix
}

// Probs returns the mixed class distribution for an image embedding.
func (h *Head) Probs(emb []float32) []float32 {
	pt := softmax(h.Text, emb, h.Temp)
	if h.Mix <= 0 {
		return h.withProbe(pt, emb)
	}
	protos := make([][]float32, len(h.Names))
	for i := range protos {
		protos[i] = h.Proto[i]
		if protos[i] == nil {
			protos[i] = h.Text[i]
		}
	}
	pp := softmax(protos, emb, h.ProtoTemp)
	for i := range pt {
		pt[i] = (1-h.Mix)*pt[i] + h.Mix*pp[i]
	}
	return h.withProbe(pt, emb)
}

func (h *Head) withProbe(pt []float32, emb []float32) []float32 {
	if h.Probe == nil || h.ProbeWeight <= 0 {
		return pt
	}
	pp := h.Probe.Probs(emb)
	for i := range pt {
		pt[i] = (1-h.ProbeWeight)*pt[i] + h.ProbeWeight*pp[i]
	}
	return pt
}

// TrainProbe fits the head's linear probe on labelled embeddings (class names must match).
func (h *Head) TrainProbe(embs [][]float32, labels []string, weight float32) {
	h.Probe = TrainProbe(h.Names, embs, labels, 300, 0.5, 1e-3)
	h.ProbeWeight = weight
}

// Classify returns the best class, its probability and the full distribution.
func (h *Head) Classify(emb []float32) (string, float32, []float32) {
	p := h.Probs(emb)
	best := 0
	for i := range p {
		if p[i] > p[best] {
			best = i
		}
	}
	return h.Names[best], p[best], p
}

func softmax(protos [][]float32, emb []float32, temp float32) []float32 {
	logits := make([]float64, len(protos))
	mx := math.Inf(-1)
	for i, p := range protos {
		logits[i] = float64(temp * Dot(p, emb))
		mx = math.Max(mx, logits[i])
	}
	var z float64
	out := make([]float32, len(protos))
	for i := range logits {
		logits[i] = math.Exp(logits[i] - mx)
		z += logits[i]
	}
	for i := range logits {
		out[i] = float32(logits[i] / z)
	}
	return out
}

// Eval is one leave-one-out accuracy run over labelled embeddings.
type Eval struct {
	Mix       float32
	ProtoTemp float32
	Correct   int
	Total     int
	Confusion map[[2]string]int // {truth, predicted} → count
	Misses    []string          // "path truth→pred p" for inspection
	// Buckets[t] counts predictions with probability ≥ t: {right, wrong}
	Buckets map[float32][2]int
}

// Thresholds reported in Buckets.
var Thresholds = []float32{0.5, 0.6, 0.7, 0.8, 0.9}

func (ev *Eval) record(truth, pred string, prob float32, path string) {
	if ev.Buckets == nil {
		ev.Buckets = map[float32][2]int{}
	}
	ev.Total++
	ok := pred == truth
	if ok {
		ev.Correct++
	} else {
		ev.Misses = append(ev.Misses, fmt.Sprintf("%s  %s→%s %.2f", path, truth, pred, prob))
	}
	ev.Confusion[[2]string{truth, pred}]++
	for _, t := range Thresholds {
		if prob >= t {
			b := ev.Buckets[t]
			if ok {
				b[0]++
			} else {
				b[1]++
			}
			ev.Buckets[t] = b
		}
	}
}

// Accuracy of the run.
func (ev Eval) Accuracy() float64 {
	if ev.Total == 0 {
		return 0
	}
	return float64(ev.Correct) / float64(ev.Total)
}

// LeaveOneOut classifies each labelled embedding with prototypes built from all the others.
// truth[i] is the label of embs[i]; paths are only used for the miss list.
func (h *Head) LeaveOneOut(embs [][]float32, truth, paths []string, mix, protoTemp float32) Eval {
	ev := Eval{Mix: mix, ProtoTemp: protoTemp, Confusion: map[[2]string]int{}}
	sums := map[string][]float32{}
	counts := map[string]int{}
	for i, e := range embs {
		if sums[truth[i]] == nil {
			sums[truth[i]] = make([]float32, len(e))
		}
		for k := range e {
			sums[truth[i]][k] += e[k]
		}
		counts[truth[i]]++
	}
	saved := h.Proto
	savedMix, savedTemp := h.Mix, h.ProtoTemp
	defer func() { h.Proto, h.Mix, h.ProtoTemp = saved, savedMix, savedTemp }()
	h.Mix, h.ProtoTemp = mix, protoTemp
	for i, e := range embs {
		h.Proto = make([][]float32, len(h.Names))
		for ci, n := range h.Names {
			s := sums[n]
			if s == nil {
				continue
			}
			p := make([]float32, len(s))
			copy(p, s)
			if n == truth[i] {
				for k := range p {
					p[k] -= e[k]
				}
				if counts[n] <= 1 {
					continue
				}
			}
			h.Proto[ci] = Normalize(p)
		}
		pred, prob, _ := h.Classify(e)
		ev.record(truth[i], pred, prob, paths[i])
	}
	sort.Strings(ev.Misses)
	return ev
}
