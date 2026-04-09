/**
 * build-icons.mjs — fetches Lucide SVG icons and writes a Go template SVG sprite.
 * Output: internal/ui/templates/partials/icons.html
 * Run:    bun run scripts/build-icons.mjs
 */

const LUCIDE_VERSION = "0.468.0";
const BASE_URL = `https://raw.githubusercontent.com/lucide-icons/lucide/${LUCIDE_VERSION}/icons`;
const OUTPUT = "internal/ui/templates/partials/icons.html";

/**
 * Maps symbol ID (used in templates as href="#icon-{key}")
 * to the corresponding Lucide icon file name.
 */
const iconMap = {
  "terminal":                   "terminal",
  "bars":                       "menu",
  "magnifying-glass":           "search",
  "gauge":                      "gauge",
  "rocket":                     "rocket",
  "gear":                       "settings",
  "moon":                       "moon",
  "sun":                        "sun",
  "circle-user":                "circle-user",
  "right-from-bracket":         "log-out",
  "server":                     "server",
  "circle-check":               "circle-check",
  "arrow-right":                "arrow-right",
  "circle-notch":               "loader-circle",
  "circle-exclamation":         "circle-alert",
  "circle-info":                "info",
  "user":                       "user",
  "lock":                       "lock",
  "xmark":                      "x",
  "arrow-left":                 "arrow-left",
  "envelope":                   "mail",
  "floppy-disk":                "save",
  "eye":                        "eye",
  "eye-slash":                  "eye-off",
  "key":                        "key",
  "link-slash":                 "unlink",
  "microchip":                  "cpu",
  "list-check":                 "list-checks",
  "triangle-exclamation":       "triangle-alert",
  "sliders":                    "sliders-horizontal",
  "upload":                     "upload",
  "plus":                       "plus",
  "code-commit":                "git-commit-horizontal",
  "code-branch":                "git-branch",
  "stopwatch":                  "timer",
  "folder":                     "folder",
  "circle-xmark":               "circle-x",
  "circle-half-stroke":         "sun-moon",
  "arrow-up-right-from-square": "external-link",
  "clock-rotate-left":          "history",
  "sort":                       "arrow-up-down",
  "sort-up":                    "arrow-up",
  "sort-down":                  "arrow-down",
  "chevron-left":               "chevron-left",
  "chevron-right":              "chevron-right",
  "angles-left":                "chevrons-left",
  "angles-right":               "chevrons-right",
  "clock":                      "clock",
  "pause":                      "pause",
  "play":                       "play",
  "scroll":                     "scroll",
  "pen":                        "pencil",
  "trash":                      "trash-2",
  "github":                     "github",
  "circle":                     "circle",
  "circle-minus":               "circle-minus",
};

async function fetchIcon(lucideName) {
  const url = `${BASE_URL}/${lucideName}.svg`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${url}`);
  return res.text();
}

function extractInner(svgText) {
  return svgText
    .replace(/<svg[^>]*>/i, "")
    .replace(/<\/svg\s*>/i, "")
    .trim();
}

const symbols = [];
const errors = [];

for (const [symbolId, lucideName] of Object.entries(iconMap)) {
  try {
    const svg = await fetchIcon(lucideName);
    const inner = extractInner(svg);
    symbols.push(
      `  <symbol id="icon-${symbolId}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">\n    ${inner}\n  </symbol>`
    );
    console.log(`✓ icon-${symbolId} (lucide:${lucideName})`);
  } catch (e) {
    errors.push(`✗ icon-${symbolId} (lucide:${lucideName}): ${e.message}`);
    console.error(`✗ icon-${symbolId} (lucide:${lucideName}): ${e.message}`);
  }
}

const sprite = `{{define "icons"}}
<svg xmlns="http://www.w3.org/2000/svg" style="display:none" aria-hidden="true">
${symbols.join("\n")}
</svg>
{{end}}`;

await Bun.write(OUTPUT, sprite);
console.log(`\nWrote ${symbols.length} icon(s) to ${OUTPUT}`);

if (errors.length > 0) {
  console.error(`\n${errors.length} error(s) — fix the icon map entries above.`);
  process.exit(1);
}
