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

  // assistant: a stateless server, a transcript kept in sessionStorage, server-sent events
  // streamed into the log. Markdown is rendered with a tiny renderer (paragraphs, lists,
  // bold, links, code); everything else is escaped.
  const chat = document.getElementById("chat"), launch = document.getElementById("chat-launch");
  if (chat && launch) {
    const log = document.getElementById("chat-log"), form = document.getElementById("chat-form"), input = document.getElementById("chat-input"),
      send = document.getElementById("chat-send"), file = document.getElementById("chat-file"), attach = document.getElementById("chat-attach"),
      attachImg = document.getElementById("chat-attach-img"), hello = log.querySelector(".chat-hello");
    const KEY = "georges.chat.v1";
    let history = [], image = null, imageType = "", busy = false;
    try { history = JSON.parse(sessionStorage.getItem(KEY) || "[]"); } catch (e) { history = []; }
    const save = () => { try { sessionStorage.setItem(KEY, JSON.stringify(history.slice(-24))); } catch (e) {} };
    const esc = (s) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
    const inline = (s) => esc(s)
      .replace(/`([^`]+)`/g, "<code>$1</code>")
      .replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>")
      .replace(/\[([^\]]+)\]\((\/[^\s)]+|https?:\/\/[^\s)]+)\)/g, '<a href="$2">$1</a>');
    const md = (text) => {
      const out = []; let list = null;
      for (const raw of text.split("\n")) {
        const line = raw.replace(/^#{1,6}\s+/, "");
        const m = line.match(/^\s*(?:[-*•]|\d+[.)])\s+(.*)$/);
        if (m) { if (!list) { list = []; } list.push("<li>" + inline(m[1]) + "</li>"); continue; }
        if (list) { out.push("<ul>" + list.join("") + "</ul>"); list = null; }
        if (line.trim()) out.push("<p>" + inline(line) + "</p>");
      }
      if (list) out.push("<ul>" + list.join("") + "</ul>");
      return out.join("");
    };
    const el = (cls, html) => { const d = document.createElement("div"); d.className = cls; if (html !== undefined) d.innerHTML = html; log.appendChild(d); return d; };
    const scroll = () => { log.scrollTop = log.scrollHeight; };
    const cards = (list) => {
      const wrap = document.createElement("div"); wrap.className = "chat-cards";
      for (const c of list) {
        const a = document.createElement("a"); a.href = c.url;
        a.innerHTML = '<div class="ci">' + (c.image ? '<img src="' + esc(c.image) + '" alt="" loading="lazy">' : "") + '</div><div class="cb"><b>' + esc(c.title) + "</b><small><span>" + esc(c.brand) + "</span><span>" + esc(c.price || "") + "</span></small></div>";
        wrap.appendChild(a);
      }
      log.appendChild(wrap);
    };
    const render = () => {
      log.querySelectorAll(".msg,.chat-cards").forEach((n) => n.remove());
      if (hello) hello.hidden = history.length > 0;
      for (const m of history) {
        if (m.role === "user") el("msg user", esc(m.content) + (m.image ? '<img src="' + m.image + '" alt="">' : ""));
        else { if (m.cards && m.cards.length) cards(m.cards); el("msg bot", md(m.content)); }
      }
      scroll();
    };
    const open = (on) => { chat.hidden = !on; launch.setAttribute("aria-expanded", on ? "true" : "false"); if (on) { render(); input.focus(); } try { sessionStorage.setItem(KEY + ".open", on ? "1" : "0"); } catch (e) {} };
    launch.addEventListener("click", () => open(true));
    document.getElementById("chat-close").addEventListener("click", () => open(false));
    document.getElementById("chat-reset").addEventListener("click", () => { history = []; save(); render(); input.focus(); });
    log.addEventListener("click", (e) => { const b = e.target.closest("[data-q]"); if (b) { input.value = b.dataset.q; form.requestSubmit(); } });
    // "Ask about this product" buttons anywhere on the page open the drawer with a question
    document.addEventListener("click", (e) => { const b = e.target.closest("[data-chat-ask]"); if (b) { open(true); input.value = b.dataset.chatAsk; form.requestSubmit(); } });
    input.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); form.requestSubmit(); } });
    input.addEventListener("input", () => { input.style.height = "auto"; input.style.height = Math.min(input.scrollHeight, 140) + "px"; });
    file.addEventListener("change", () => {
      const f = file.files[0]; if (!f) return;
      // downscale in the browser: the matcher wants 224 px, the model wants nothing bigger than ~1 MB
      const img = new Image(); const url = URL.createObjectURL(f);
      img.onload = () => {
        const max = 1024, k = Math.min(1, max / Math.max(img.width, img.height));
        const c = document.createElement("canvas"); c.width = Math.round(img.width * k); c.height = Math.round(img.height * k);
        c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
        const data = c.toDataURL("image/jpeg", 0.88);
        image = data.split(",")[1]; imageType = "image/jpeg";
        attachImg.src = data; attach.hidden = false; URL.revokeObjectURL(url); input.focus();
      };
      img.src = url; file.value = "";
    });
    document.getElementById("chat-attach-x").addEventListener("click", () => { image = null; attach.hidden = true; });
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const q = input.value.trim();
      if (busy || (!q && !image)) return;
      busy = true; send.disabled = true;
      const userMsg = { role: "user", content: q || "What do you carry that looks like this?" };
      if (image) userMsg.image = "data:" + imageType + ";base64," + image;
      history.push(userMsg); save();
      if (hello) hello.hidden = true;
      el("msg user", esc(userMsg.content) + (userMsg.image ? '<img src="' + userMsg.image + '" alt="">' : ""));
      input.value = ""; input.style.height = "auto";
      const body = { messages: history.map((m) => ({ role: m.role, content: m.content, shown: m.shown })), rep: chat.hasAttribute("data-rep"), page: location.pathname };
      if (image) { body.image = image; body.image_type = imageType; image = null; attach.hidden = true; }
      let status = el("msg status", "Thinking…"), bot = null, text = "", shownCards = [], shown = [];
      scroll();
      const finish = (err) => {
        if (status) { status.remove(); status = null; }
        if (err) el("msg error", esc(err));
        history.push({ role: "assistant", content: text, shown, cards: shownCards }); save();
        busy = false; send.disabled = false; scroll(); input.focus();
      };
      try {
        const res = await fetch("/chat", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
        if (!res.ok) { let msg = "The assistant is unavailable right now."; try { msg = (await res.json()).error || msg; } catch (e) {} finish(msg); return; }
        const reader = res.body.getReader(), dec = new TextDecoder(); let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          let i;
          while ((i = buf.indexOf("\n\n")) >= 0) {
            const chunk = buf.slice(0, i); buf = buf.slice(i + 2);
            if (!chunk.startsWith("data: ")) continue;
            let ev; try { ev = JSON.parse(chunk.slice(6)); } catch (e) { continue; }
            if (ev.type === "status") { if (!status) status = el("msg status"); status.textContent = ev.text; log.appendChild(status); }
            else if (ev.type === "cards") { shownCards = shownCards.concat(ev.cards); cards(ev.cards); if (bot) log.appendChild(bot); if (status) log.appendChild(status); }
            else if (ev.type === "delta") { if (status) { status.remove(); status = null; } if (!bot) bot = el("msg bot"); text += ev.text; bot.innerHTML = md(text); }
            else if (ev.type === "error") { finish(ev.error); return; }
            else if (ev.type === "done") { shown = ev.shown || []; }
            scroll();
          }
        }
        finish();
      } catch (err) { finish("Connection lost. Please try again."); }
    });
    try { if (sessionStorage.getItem(KEY + ".open") === "1") open(true); } catch (e) {}
  }
})();
