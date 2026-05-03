// Tiny vanilla JS for dashboard polish: copy buttons + in-pane search.
// No frameworks, no build step — same posture as htmx.min.js.
(function () {
  "use strict";

  // ─────────────────────── Copy buttons ───────────────────────
  // <button class="copy-btn" data-copy-target=".body-pane">Copy</button>
  // — looks for `.body-pane` inside the closest .panel, copies its
  // textContent (which strips highlight spans, leaving raw JSON text).
  document.addEventListener("click", function (e) {
    var btn = e.target.closest(".copy-btn");
    if (!btn) return;
    e.preventDefault();
    var sel = btn.dataset.copyTarget || ".body-pane";
    var scope = btn.closest(".panel") || document;
    var target = scope.querySelector(sel);
    if (!target) return;

    var text = target.textContent.replace(/\s+$/g, "");
    copy(text)
      .then(function () { feedback(btn, "Copied"); })
      .catch(function () { feedback(btn, "Failed"); });
  });

  function copy(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      try {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        var ok = document.execCommand("copy");
        document.body.removeChild(ta);
        ok ? resolve() : reject(new Error("execCommand failed"));
      } catch (err) {
        reject(err);
      }
    });
  }

  function feedback(btn, label) {
    if (!btn.dataset.origLabel) btn.dataset.origLabel = btn.textContent;
    btn.textContent = label;
    btn.classList.add("copy-btn-feedback");
    clearTimeout(btn._copyTimer);
    btn._copyTimer = setTimeout(function () {
      btn.textContent = btn.dataset.origLabel;
      btn.classList.remove("copy-btn-feedback");
    }, 1200);
  }

  // ─────────────────────── Find in payload ───────────────────────
  // Wraps case-insensitive matches of the search query in <mark> elements
  // across every text node under [data-search-scope]. Tracks the active
  // match for prev/next navigation; ESC clears.
  var searchInput = document.getElementById("body-search");
  if (searchInput) {
    var scope = document.querySelector("[data-search-scope]");
    var countEl = document.querySelector(".body-search-count");
    var matches = [];
    var current = -1;
    var lastQuery = "";

    function clearHighlights() {
      if (!scope) return;
      var marks = scope.querySelectorAll("mark.search-hit");
      marks.forEach(function (m) {
        var parent = m.parentNode;
        parent.replaceChild(document.createTextNode(m.textContent), m);
        parent.normalize();
      });
    }

    function highlight(query) {
      matches = [];
      current = -1;
      clearHighlights();
      if (!scope || !query) {
        updateCounter();
        return;
      }
      var lc = query.toLowerCase();
      var walker = document.createTreeWalker(scope, NodeFilter.SHOW_TEXT, {
        acceptNode: function (node) {
          // Skip text inside hidden script/style and inside our own search bar.
          var p = node.parentNode;
          while (p && p !== scope) {
            if (p.classList && p.classList.contains("body-search-bar")) {
              return NodeFilter.FILTER_REJECT;
            }
            p = p.parentNode;
          }
          return node.nodeValue && node.nodeValue.toLowerCase().includes(lc)
            ? NodeFilter.FILTER_ACCEPT
            : NodeFilter.FILTER_REJECT;
        },
      });
      var nodes = [];
      var n;
      while ((n = walker.nextNode())) nodes.push(n);
      nodes.forEach(function (node) {
        var text = node.nodeValue;
        var tl = text.toLowerCase();
        var frag = document.createDocumentFragment();
        var i = 0;
        var idx;
        while ((idx = tl.indexOf(lc, i)) !== -1) {
          if (idx > i) {
            frag.appendChild(document.createTextNode(text.substring(i, idx)));
          }
          var mark = document.createElement("mark");
          mark.className = "search-hit";
          mark.textContent = text.substring(idx, idx + lc.length);
          frag.appendChild(mark);
          matches.push(mark);
          i = idx + lc.length;
        }
        if (i < text.length) {
          frag.appendChild(document.createTextNode(text.substring(i)));
        }
        node.parentNode.replaceChild(frag, node);
      });
      if (matches.length > 0) jumpTo(0, false);
      updateCounter();
    }

    function jumpTo(index, scroll) {
      if (matches.length === 0) return;
      matches.forEach(function (m) { m.classList.remove("search-hit-current"); });
      current = ((index % matches.length) + matches.length) % matches.length;
      var cur = matches[current];
      cur.classList.add("search-hit-current");
      // Expand any collapsed <details> ancestors so the match is visible.
      var p = cur.parentNode;
      while (p && p !== scope) {
        if (p.tagName === "DETAILS") p.open = true;
        p = p.parentNode;
      }
      if (scroll !== false) {
        cur.scrollIntoView({ block: "center", behavior: "smooth" });
      }
      updateCounter();
    }

    function updateCounter() {
      if (!countEl) return;
      if (!searchInput.value) { countEl.textContent = "—"; return; }
      if (matches.length === 0) { countEl.textContent = "0"; return; }
      countEl.textContent = (current + 1) + " / " + matches.length;
    }

    function onInput() {
      var q = searchInput.value;
      if (q === lastQuery) return;
      lastQuery = q;
      highlight(q);
    }

    // Debounce so typing fast doesn't thrash the DOM.
    var debounceTimer;
    searchInput.addEventListener("input", function () {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(onInput, 80);
    });

    searchInput.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        searchInput.value = "";
        lastQuery = "";
        highlight("");
        searchInput.blur();
      } else if (e.key === "Enter") {
        e.preventDefault();
        if (matches.length > 0) jumpTo(current + (e.shiftKey ? -1 : 1), true);
      }
    });

    document.addEventListener("click", function (e) {
      var nav = e.target.closest("[data-search-nav]");
      if (!nav) return;
      e.preventDefault();
      if (matches.length === 0) return;
      jumpTo(current + (nav.dataset.searchNav === "prev" ? -1 : 1), true);
    });

    // Cmd/Ctrl-F focuses our input instead of the browser find.
    document.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "f" && scope) {
        e.preventDefault();
        searchInput.focus();
        searchInput.select();
      }
    });
  }
})();
