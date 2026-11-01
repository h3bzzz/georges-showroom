package vision

// Tone classes: what colour of metal (or material) the pictured product is. Chrome, nickel
// and stainless all read as silver to a camera, so the classes are coarser than finish names;
// store.ToneGroup folds finish tones into them. There is no gray class: brushed steel and
// gunmetal are too close for CLIP, so gunmetal finishes count as silver for pictures. Scored from the stored CLIP embeddings by
// `catalogs vision --tone`, with prototypes from crops linked to SKUs of a known finish.
var ToneClasses = buildTone()

var toneThings = []string{"faucet", "shower head", "door lever", "cabinet knob", "towel bar", "sink", "plumbing fixture"}

func buildTone() []Class {
	return []Class{
		{Name: "silver", Prompts: expand([]string{
			"a photo of a polished chrome %s", "a photo of a brushed nickel %s", "a photo of a stainless steel %s",
			"a silver-coloured %s with a mirror shine", "a %s in a bright silver metal finish",
		}, toneThings)},
		{Name: "gold", Prompts: expand([]string{
			"a photo of a polished brass %s", "a photo of a brushed gold %s", "a photo of a satin brass %s",
			"a %s in a warm yellow gold metal finish", "a photo of an unlacquered brass %s",
		}, toneThings)},
		{Name: "bronze", Prompts: expand([]string{
			"a photo of an oil rubbed bronze %s", "a photo of a dark brown bronze %s", "a photo of an antique bronze %s",
			"a %s in a dark chocolate brown metal finish", "a photo of a venetian bronze %s with a rubbed patina",
		}, toneThings)},
		{Name: "black", Prompts: expand([]string{
			"a photo of a matte black %s", "a photo of a flat black %s", "a %s in a black metal finish",
		}, toneThings)},
		{Name: "white", Prompts: expand([]string{
			"a photo of a white %s", "a photo of a white porcelain %s", "a %s in a gloss white finish",
		}, toneThings, "a photo of a white ceramic toilet", "a photo of a white porcelain sink")},
		{Name: "copper", Prompts: expand([]string{
			"a photo of a polished copper %s", "a photo of a rose gold %s", "a %s in a pinkish copper metal finish",
		}, toneThings)},
		{Name: "other", Prompts: []string{
			"a photo of a wooden vanity cabinet", "a photo of a coloured painted product", "a photo of a green or blue product",
			"a photo of a kitchen scene with many colours", "a line drawing", "a page of text", "a photo of a person",
			"a black and white photograph", "a black silhouette on white", "a portrait of a man", "a logo",
			"a photo of a plant", "a photo of a room interior", "a grayscale photo",
		}},
	}
}
