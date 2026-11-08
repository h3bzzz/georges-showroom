package taxonomy

import "regexp"

// Tiers order families: fixtures lead, parts follow, junk-titled rows sink.
const (
	TierFixture  = 0
	TierOrdinary = 1
	TierPart     = 2
	TierJunk     = 3
)

var (
	rePart             = regexp.MustCompile(`(?i)(rough|cartridge|\bparts?\b|aerator|\bstems?\b|escutcheon|flange|adapter|extension|\bhoses?\b|bracket|mounting|repair|spare|valve body|\bbody only|rough body|supply|\bstops?\b|\bkit\b|spindle|strike|latch|template|replacement|valve only|in-wall|rough-in|diverter valve|\breplac|battery|power supply|power pack)`)
	reJunk             = regexp.MustCompile(`(?i)[:_"(]|\d{4,}|\d[A-Za-z]|\.$|^(all|have|with|no|the|and|or|for|only|type)\b|\b(only|holes|interior|depth|overview|stock|special order|continued|warranty|guarantee|care guide|care kit|cleaning|see page|see pages|price list|pricelist|index)\b|\binclude .* with any\b`)
	reHardwareFixture  = regexp.MustCompile(`(?i)(knob|pull|lever|handleset|entry|hinge|deadbolt|cabinet|door)`)
	reApplianceFixture = regexp.MustCompile(`(?i)(range|refrigerat|dishwasher|oven|cooktop|hood|wine|freezer|ice maker|coffee|washer|dryer)`)
	rePlumbingFixture  = regexp.MustCompile(`(?i)(faucet|\bsinks?\b|basin|lavatory|bathtub|\btubs?\b|freestanding|shower system|showerhead|shower head|rain head|rainhead|toilet|vanit|mirror|pot filler|bidet|tub filler|column|filter system|filtration system|medicine cabinet)`)
	rePlumbingNot      = regexp.MustCompile(`(?i)\b(grid|strainer|holder|basket|hook|grip|spout|overview|stock|special order|food service|drain|stopper|cap|plug|handle|handles|lever|knob|trim|hole|holes|only|accessor|clips?|screws?|compatible|coordinating|waste|overflow|listed|cover|fitting|fittings)\b`)
)

// TierFor ranks a family from its room, title, product type and category.
func TierFor(room, title, productType, category string) int {
	s := title + " " + productType + " " + category
	if rePart.MatchString(s) {
		return TierPart
	}
	if title == "" || len(title) > 70 || reJunk.MatchString(title) {
		return TierJunk
	}
	switch room {
	case "hardware":
		if reHardwareFixture.MatchString(s) {
			return TierFixture
		}
	case "appliances":
		if reApplianceFixture.MatchString(s) {
			return TierFixture
		}
	case "bathroom", "kitchen":
		if rePlumbingFixture.MatchString(title) && !rePlumbingNot.MatchString(title) {
			return TierFixture
		}
	}
	return TierOrdinary
}
