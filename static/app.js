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

// Drag rows of a [data-sortable] table by their .drag handle (mouse or touch),
// or move them with the arrow keys; the new order is posted as ids=a,b,c.
(function () {
  const table = document.querySelector("table[data-sortable]");
  if (!table) return;
  const body = table.tBodies[0];
  const rows = () => [...body.querySelectorAll("tr[data-id]")];
  let saved = rows().map((r) => r.dataset.id).join(",");

  function save() {
    const ids = rows().map((r) => r.dataset.id).join(",");
    if (ids === saved) return;
    table.classList.add("saving");
    fetch(table.dataset.sortable, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "ids=" + encodeURIComponent(ids),
    }).then((r) => {
      table.classList.remove("saving");
      if (!r.ok) throw new Error(r.status);
      saved = ids;
    }).catch(() => {
      table.classList.remove("saving");
      table.classList.add("save-failed");
      setTimeout(() => location.reload(), 1500);
    });
  }

  let drag = null;
  body.addEventListener("pointerdown", (e) => {
    const handle = e.target.closest(".drag");
    if (!handle || e.button > 0) return;
    const row = handle.closest("tr");
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    drag = { row };
    row.classList.add("dragging");
  });
  body.addEventListener("pointermove", (e) => {
    if (!drag) return;
    const y = e.clientY;
    // Move the row past any neighbour whose middle the pointer has crossed.
    for (const other of rows()) {
      if (other === drag.row) continue;
      const box = other.getBoundingClientRect();
      const mid = box.top + box.height / 2;
      const below = drag.row.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_FOLLOWING;
      if (below && y > mid) other.after(drag.row);
      else if (!below && y < mid) other.before(drag.row);
    }
  });
  const end = () => {
    if (!drag) return;
    drag.row.classList.remove("dragging");
    drag = null;
    save();
  };
  body.addEventListener("pointerup", end);
  body.addEventListener("pointercancel", end);

  body.addEventListener("keydown", (e) => {
    const handle = e.target.closest(".drag");
    if (!handle || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    const row = handle.closest("tr");
    if (e.key === "ArrowUp" && row.previousElementSibling) row.previousElementSibling.before(row);
    if (e.key === "ArrowDown" && row.nextElementSibling) row.nextElementSibling.after(row);
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
