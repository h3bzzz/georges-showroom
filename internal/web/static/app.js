// George's v2: the few behaviours htmx does not cover. ~2 KB, no dependencies.
(function () {
  "use strict";
  // typeahead keyboard: arrows move, Enter follows, Escape closes
  const q = document.getElementById("q"), box = document.getElementById("suggest");
  if (q && box) {
    let i = -1;
    const items = () => Array.from(box.querySelectorAll("li[role=option]"));
    q.addEventListener("keydown", (e) => {
      const it = items();
      if (!it.length) return;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        it.forEach((li) => li.classList.remove("active"));
        i = (i + (e.key === "ArrowDown" ? 1 : -1) + it.length) % it.length;
        it[i].classList.add("active");
        it[i].scrollIntoView({ block: "nearest" });
      } else if (e.key === "Enter" && i >= 0) {
        e.preventDefault();
        it[i].querySelector("a").click();
      } else if (e.key === "Escape") {
        box.innerHTML = "";
        i = -1;
      }
    });
    document.addEventListener("click", (e) => { if (!e.target.closest(".search")) box.innerHTML = ""; });
    box.addEventListener("htmx:afterSwap", () => { i = -1; q.setAttribute("aria-expanded", box.children.length ? "true" : "false"); });
  }
  // product gallery: thumbnails swap the stage; a finish button swaps to the real photo of
  // that finish when one exists, and says so when it does not — nothing is recoloured
  function wireGallery(root) {
    const stage = root.querySelector("#stage"), img = root.querySelector("#stage-img"), cap = root.querySelector("#shown-in");
    if (!stage || !img) return;
    const ladder = (h) => "/a/" + h.slice(0, 16) + "/";
    const show = (d) => {
      img.src = d.src; img.srcset = d.srcset || ""; stage.href = d.full;
      if (d.w && d.w !== "0") { img.width = d.w; img.height = d.h; }
      if (cap) cap.textContent = d.label || "";
      root.querySelectorAll(".thumbs [role=tab]").forEach((t) => t.setAttribute("aria-selected", t.dataset.hash === d.hash ? "true" : "false"));
    };
    root.querySelectorAll(".thumbs [role=tab]").forEach((b) => b.addEventListener("click", () => show(b.dataset)));
    const note = root.querySelector("#finish-note");
    root.querySelectorAll(".finish").forEach((b) => b.addEventListener("click", () => {
      root.querySelectorAll(".finish").forEach((x) => x.classList.toggle("on", x === b));
      const h = b.dataset.hash;
      if (h) {
        const base = ladder(h);
        show({ hash: h, src: base + "960.webp", srcset: base + "320.webp 320w, " + base + "640.webp 640w, " + base + "960.webp 960w, " + base + "1600.webp 1600w",
               full: base + "1600.webp", w: b.dataset.w, h: b.dataset.h, label: "Shown in " + b.dataset.finish });
        if (note) note.hidden = true;
      } else if (note) {
        note.textContent = "No photograph of " + b.dataset.finish + " on file yet — the picture shows " + ((cap && cap.textContent.trim()) || "another finish").replace(/^Shown in /, "").replace(/^(Manufacturer photo|From the catalog|From the spec sheet)$/, "another finish") + ".";
        note.hidden = false;
      }
    }));
    const lb = document.getElementById("lightbox");
    if (lb && typeof lb.showModal === "function") {
      stage.addEventListener("click", (e) => { e.preventDefault(); lb.querySelector("img").src = stage.href; lb.showModal(); });
      lb.addEventListener("click", () => lb.close());
    }
  }
  // facets: open on wide screens, collapsed into chips on phones
  function wireFacets(root) {
    const wide = window.matchMedia("(min-width:1024px)").matches;
    root.querySelectorAll(".facet[data-open]").forEach((d) => { d.open = wide; });
  }
  wireGallery(document); wireFacets(document);
  document.addEventListener("htmx:afterSettle", (e) => { wireGallery(e.target); wireFacets(e.target); });
})();
