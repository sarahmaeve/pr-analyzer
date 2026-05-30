// Enrichment loader for the pr-analyzer report.
//
// Reads the optional sidecar (window.__praEnrichment, set by pr-scan.js
// pulled in as a sibling script tag — file://-safe, unlike fetch) and,
// for each PR present, appends a deep-findings section into that PR's
// drill-down body using the
// report's own pra-* classes. No sidecar → no-op, so index.html renders
// identically whether or not anything was scanned. Builds nodes with
// textContent (never innerHTML) so finding strings can't inject markup.
(function () {
  "use strict";
  var data = window.__praEnrichment;
  if (!data || typeof data !== "object") {
    return;
  }

  var TIER_RANK = { info: 0, success: 1, warning: 2, danger: 3 };
  function normTier(tier) {
    return tier in TIER_RANK ? tier : "info";
  }
  function pillClass(tier) {
    return "pra-pill pra-pill-" + normTier(tier);
  }

  function deepSection(sec) {
    var section = document.createElement("section");
    section.className = "pra-pr-deep";

    var title = document.createElement("h3");
    title.className = "pra-pr-deep-title";
    title.textContent = sec.title || "Deep findings";
    section.appendChild(title);

    if (sec.pills && sec.pills.length) {
      var pills = document.createElement("p");
      pills.className = "pra-pr-deep-pills";
      sec.pills.forEach(function (pill) {
        var span = document.createElement("span");
        span.className = pillClass(pill.tier);
        span.textContent = pill.text;
        pills.appendChild(span);
        pills.appendChild(document.createTextNode(" "));
      });
      section.appendChild(pills);
    }

    if (sec.rows && sec.rows.length) {
      var dl = document.createElement("dl");
      dl.className = "pra-pr-signals";
      sec.rows.forEach(function (row) {
        var dt = document.createElement("dt");
        dt.textContent = row.term;
        var dd = document.createElement("dd");
        dd.textContent = row.detail;
        dl.appendChild(dt);
        dl.appendChild(dd);
      });
      section.appendChild(dl);
    }

    return section;
  }

  // flagRow marks a scanned PR's always-visible summary row so the
  // collapsed overview shows, at a glance, which PRs were pr-scanned and
  // their headline verdict — without expanding the drill-down. topPill is
  // the most-severe pill across the PR's sections (BLOCK/AUTHOR-BURNED >
  // WARN > CLEAR).
  function flagRow(li, topPill) {
    li.classList.add("pra-pr-scanned");
    if (!topPill) {
      return;
    }
    var tier = normTier(topPill.tier);
    li.classList.add("pra-pr-scanned-" + tier);
    var summary = li.querySelector("summary");
    if (!summary || summary.querySelector(".pra-pr-scan-flag")) {
      return;
    }
    var badge = document.createElement("span");
    badge.className = "pra-pill pra-pill-" + tier + " pra-pr-scan-flag";
    badge.title = "pr-scan deep findings";
    badge.textContent = topPill.text;
    summary.appendChild(badge);
  }

  document.querySelectorAll(".pra-pr").forEach(function (li) {
    var sections = data[li.getAttribute("data-pra-pr-number")];
    if (!sections || !sections.length) {
      return;
    }
    var body = li.querySelector(".pra-pr-body");
    if (!body) {
      return;
    }
    var topPill = null;
    sections.forEach(function (sec) {
      body.appendChild(deepSection(sec));
      (sec.pills || []).forEach(function (pill) {
        if (!topPill || TIER_RANK[normTier(pill.tier)] > TIER_RANK[normTier(topPill.tier)]) {
          topPill = pill;
        }
      });
    });
    flagRow(li, topPill);
  });
})();
