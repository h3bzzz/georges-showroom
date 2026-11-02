#!/usr/bin/env bash
# Image-QA sampler: pull a handful of live shop pages, gather every card picture they show and
# lay them out on numbered contact sheets with an index, so a reviewer (a person, or Claude
# looking at the sheet) can point at a bad card and refuse its crop:
#
#   deploy/qa-sample.sh [N]          N random pages (default 4) → $OUT/sheet-*.jpg + index.tsv
#   georges catalogs reject <path>   for each bad card (index.tsv has the path per number)
#   deploy/sync-data.sh db           publish the re-pick
#
# Pages come from a pool of category and search URLs plus the first five pages of a random
# category, which is what shoppers actually land on.
set -euo pipefail
cd "$(dirname "$0")/.."
SITE=${SITE:-https://georgesshowroom.tech}
OUT=${OUT:-/tmp/georges-qa}
N=${1:-4}
mkdir -p "$OUT" && rm -f "$OUT"/sheet-*.jpg "$OUT"/index.tsv "$OUT"/pages.txt
pool=(
  "/shop/bathroom?category=Faucets" "/shop/bathroom?category=Hand+Showers" "/shop/bathroom?category=Bathroom+Sinks"
  "/shop/bathroom?category=Toilets" "/shop/bathroom?category=Bathtubs" "/shop/bathroom?category=Shower+Heads"
  "/shop/kitchen?category=Kitchen+Faucets" "/shop/kitchen?category=Kitchen+Sinks" "/shop/hardware"
  "/shop/bathroom?category=Towel+Bars+%26+Rings" "/shop/bathroom?category=Medicine+Cabinets" "/shop/bathroom?category=Vanities"
  "/shop?q=brushed+nickel+faucet" "/shop?q=matte+black" "/shop?q=freestanding+tub" "/shop?q=pull+down+kitchen+faucet"
  "/shop?q=polished+brass" "/shop?q=widespread" "/shop?q=handshower" "/shop?q=rain+head"
)
for i in $(shuf -i 0-$((${#pool[@]}-1)) -n "$N"); do
  p=${pool[$i]}; pg=$((RANDOM % 5 + 1))
  sep='?'; [[ $p == *\?* ]] && sep='&'
  echo "$p${sep}page=$pg" >> "$OUT/pages.txt"
done
: > "$OUT/paths.txt"
while read -r url; do
  curl -s "$SITE$url" | { grep -oE '/img/640/[^" ]+' || true; } | sed 's#^/img/640/##' | sort -u | sed "s#^#$url\t#" >> "$OUT/paths.txt"
done < "$OUT/pages.txt"
n=0; args=(); sheet=1
while IFS=$'\t' read -r url path; do
  n=$((n+1)); printf '%03d\t%s\t%s\n' "$n" "$path" "$url" >> "$OUT/index.tsv"
  f="${DATA_DIR:-data/raw}/$path"
  if [ -f "$f" ]; then args+=(-label "$n" "$f"); else args+=(-label "$n?" "xc:#eee"); fi
  if [ ${#args[@]} -ge 120 ]; then
    montage "${args[@]}" -tile 10x -geometry 150x150+3+3 -font Adwaita-Sans -pointsize 16 -fill black -background white "$OUT/sheet-$sheet.jpg"; sheet=$((sheet+1)); args=()
  fi
done < "$OUT/paths.txt"
if [ ${#args[@]} -gt 0 ]; then
  montage "${args[@]}" -tile 10x -geometry 150x150+3+3 -font Adwaita-Sans -pointsize 16 -fill black -background white "$OUT/sheet-$sheet.jpg"
fi
echo "$n cards from $(wc -l < "$OUT/pages.txt") pages → $OUT/sheet-*.jpg, index in $OUT/index.tsv"
