// Progressive enhancement only: every page works without this file.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) e.preventDefault();
});

// Clipboard API needs a secure context (https or localhost); on plain http the
// hidden-textarea copy is the fallback.
function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise((resolve, reject) => {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    ok ? resolve() : reject(new Error("copy failed"));
  });
}

function flash(el) {
  const label = el.querySelector(".addr-label") || el;
  if (label.dataset.orig === undefined) label.dataset.orig = label.textContent;
  label.textContent = "✓ " + (el.dataset.copied || "OK");
  el.classList.add("copied");
  clearTimeout(el._copyTimer);
  el._copyTimer = setTimeout(() => {
    label.textContent = label.dataset.orig;
    el.classList.remove("copied");
  }, 1500);
}

document.addEventListener("click", (e) => {
  const copy = e.target.closest("[data-copy], [data-copy-value]");
  if (copy) {
    let text = copy.dataset.copyValue;
    if (text === undefined) {
      const el = document.getElementById(copy.dataset.copy);
      text = el ? el.textContent.trim() : "";
    }
    if (text) copyText(text).then(() => flash(copy), () => {});
    return;
  }
  const tab = e.target.closest("[data-region]");
  if (tab) {
    document.querySelectorAll("[data-region]").forEach((t) => t.classList.toggle("active", t === tab));
    document.querySelectorAll("[data-region-panel]").forEach((p) => {
      p.hidden = p.dataset.regionPanel !== tab.dataset.region;
    });
  }
});

// Drag rows of the admin list by their .drag handle (mouse or touch), or move
// them with the arrow keys. The list has two groups (tbody[data-group]): "hot"
// (★ really want) on top and "rest" below; a row moved between them gets or
// loses ★. The new order is posted as ids=a,b,c&hot=a; the server also
// switches the list to manual order, so the page reloads when the sort
// control or a ★ has to change.
(function () {
  const table = document.querySelector("table[data-sortable]");
  if (!table) return;
  const hotBody = table.querySelector('tbody[data-group="hot"]');
  const restBody = table.querySelector('tbody[data-group="rest"]');
  const restHead = restBody && restBody.querySelector("tr.group");
  const rows = () => [...table.querySelectorAll("tr[data-id]")];
  const hotIds = () => (hotBody ? [...hotBody.querySelectorAll("tr[data-id]")].map((r) => r.dataset.id) : []);
  const state = () => rows().map((r) => r.dataset.id).join(",") + "|" + hotIds().join(",");
  let saved = state();
  const sortSelect = document.getElementById("sort");

  function save() {
    const now = state();
    if (now === saved) return;
    const groupChanged = now.split("|")[1] !== saved.split("|")[1];
    const wasSorted = sortSelect && sortSelect.value !== "";
    table.classList.add("saving");
    fetch(table.dataset.sortable, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "ids=" + encodeURIComponent(now.split("|")[0]) + "&hot=" + encodeURIComponent(now.split("|")[1]),
    }).then((r) => {
      table.classList.remove("saving");
      if (!r.ok) throw new Error(r.status);
      saved = now;
      if (groupChanged || wasSorted) location.reload();
    }).catch(() => {
      table.classList.remove("saving");
      table.classList.add("save-failed");
      setTimeout(() => location.reload(), 1500);
    });
  }

  function markTarget(row) {
    for (const b of [hotBody, restBody]) if (b) b.classList.toggle("drop-here", !!row && row.parentNode === b);
  }

  // The dragged row follows the pointer (translateY from its place in the
  // list); the list itself is reordered whenever the pointer crosses the
  // middle of a neighbour or the line between the two groups.
  let drag = null;
  table.addEventListener("pointerdown", (e) => {
    const handle = e.target.closest(".drag");
    if (!handle || e.button > 0) return;
    const row = handle.closest("tr");
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    drag = { row, grab: e.clientY - row.getBoundingClientRect().top };
    row.classList.add("dragging");
    table.classList.add("is-dragging");
    markTarget(row);
  });
  table.addEventListener("pointermove", (e) => {
    if (!drag) return;
    const y = e.clientY;
    const row = drag.row;
    const order = () => rows().map((r) => r.dataset.id).join(",") + "|" + row.parentNode.dataset.group;
    const was = order();
    const tops = new Map(rows().map((r) => [r, r.getBoundingClientRect().top]));
    for (const other of rows()) {
      if (other === row) continue;
      const box = other.getBoundingClientRect();
      const mid = box.top + box.height / 2;
      const below = row.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_FOLLOWING;
      if (below && y > mid) other.after(row);
      else if (!below && y < mid) other.before(row);
    }
    if (restHead) {
      const box = restHead.getBoundingClientRect();
      const mid = box.top + box.height / 2;
      if (row.parentNode === hotBody && y > mid && !restHead.nextElementSibling?.matches("tr[data-id]")) restHead.after(row);
      else if (row.parentNode === restBody && y < mid) hotBody.append(row);
    }
    // Neighbours that moved slide from their old place to the new one.
    if (order() !== was) {
      for (const [r, oldTop] of tops) {
        if (r === row) continue;
        const d = oldTop - r.getBoundingClientRect().top;
        if (!d) continue;
        r.style.transition = "none";
        r.style.transform = "translateY(" + d + "px)";
        requestAnimationFrame(() => { r.style.transition = ""; r.style.transform = ""; });
      }
    }
    row.style.transform = "";
    const top = row.getBoundingClientRect().top;
    row.style.transform = "translateY(" + (y - drag.grab - top) + "px)";
    markTarget(row);
  });
  const end = () => {
    if (!drag) return;
    drag.row.classList.remove("dragging");
    drag.row.style.transform = "";
    table.classList.remove("is-dragging");
    markTarget(null);
    drag = null;
    save();
  };
  table.addEventListener("pointerup", end);
  table.addEventListener("pointercancel", end);

  table.addEventListener("keydown", (e) => {
    const handle = e.target.closest(".drag");
    if (!handle || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    const row = handle.closest("tr");
    const prev = row.previousElementSibling, next = row.nextElementSibling;
    if (e.key === "ArrowUp") {
      if (prev && prev.matches("tr[data-id]")) prev.before(row);
      else if (row.parentNode === restBody && hotBody) hotBody.append(row);
    } else {
      if (next && next.matches("tr[data-id]")) next.after(row);
      else if (row.parentNode === hotBody && restHead) restHead.after(row);
    }
    handle.focus();
    save();
  });
})();

// The reserve button stays disabled until the required email looks valid.
// Only script turns it off, so without JavaScript the browser's own
// "required" check still applies.
document.querySelectorAll("form[data-require-email]").forEach((form) => {
  const input = form.querySelector('input[name="email"]');
  const button = form.querySelector("button");
  if (!input || !button) return;
  const update = () => { button.disabled = !(input.value.trim() && input.checkValidity()); };
  input.addEventListener("input", update);
  update();
});
