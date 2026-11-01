package vision

// SceneClasses separate a product photograph (the thing itself, on white or installed in a
// tight shot) from a room interior or mood shot that happens to contain it. A price book opens
// every collection with such a scene; it is a fine gallery image and a poor card: a bidet's
// card must show a bidet, not a bathroom. Scored from the stored embeddings like the tone
// head (`catalogs vision --scene`).
var SceneClasses = buildScene()

var sceneThings = []string{"faucet", "sink", "bathtub", "toilet", "shower head", "door lever", "cabinet pull", "towel bar", "vanity", "plumbing fixture"}

func buildScene() []Class {
	return []Class{
		{Name: "product", Prompts: expand([]string{
			"a product photo of a single %s on a white background",
			"a studio photo of one %s filling the frame",
			"a close-up catalog photo of a %s installed, nothing else in the picture",
			"a cut-out photo of a %s",
		}, sceneThings)},
		{Name: "scene", Prompts: []string{
			"an interior design photograph of an entire bathroom",
			"a wide-angle photo of a kitchen with cabinets, countertops and a window",
			"an architectural photo of a bathroom with a bathtub, a vanity and tiled walls",
			"a room with furniture, a floor and a window",
			"a lifestyle photo of a luxury bathroom interior",
			"a photo of a shower room with glass walls and a bench",
			"a photo of a hallway with a door",
			"a photo of a house exterior",
			"a mood board of materials and colours",
			"a landscape photo",
		}},
	}
}
