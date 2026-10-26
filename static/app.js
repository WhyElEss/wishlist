// Progressive enhancement only: every page works without this file.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) e.preventDefault();
});

document.addEventListener("click", (e) => {
  const copy = e.target.closest("[data-copy]");
  if (copy) {
    const el = document.getElementById(copy.dataset.copy);
    if (el && navigator.clipboard) {
      navigator.clipboard.writeText(el.textContent.trim()).then(() => {
        const label = copy.textContent;
        copy.textContent = copy.dataset.copied || "OK";
        setTimeout(() => { copy.textContent = label; }, 1500);
      });
    }
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
