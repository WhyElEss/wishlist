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
