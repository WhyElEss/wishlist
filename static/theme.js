// Theme, as on the blog: "auto" follows the system; "light"
// or "dark" is the reader's own choice, kept in localStorage. Loaded in
// <head> so the page never flashes the other theme; the header button
// (#theme-btn) goes auto → light → dark.
(function () {
  var KEY = "theme";
  var root = document.documentElement;
  function read() {
    try {
      var v = localStorage.getItem(KEY);
      return v === "light" || v === "dark" ? v : "auto";
    } catch (e) {
      return "auto";
    }
  }
  function apply(v) {
    if (v === "auto") delete root.dataset.theme;
    else root.dataset.theme = v;
    root.dataset.themeChoice = v;
  }
  apply(read());

  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.getElementById("theme-btn");
    if (!btn) return;
    function label(v) {
      var l = btn.dataset[v];
      if (l) {
        btn.setAttribute("aria-label", l);
        btn.title = l;
      }
    }
    label(read());
    btn.addEventListener("click", function () {
      var order = ["auto", "light", "dark"];
      var v = order[(order.indexOf(read()) + 1) % order.length];
      try {
        if (v === "auto") localStorage.removeItem(KEY);
        else localStorage.setItem(KEY, v);
      } catch (e) {}
      apply(v);
      label(v);
    });
  });
})();
