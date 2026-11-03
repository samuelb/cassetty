// Skins, the platform's download key, the latest version and copy buttons.

const REPO = "samuelb/cassetty";
// The order `t` cycles through in cassetty, starting at its default.
const SKINS = ["sunbleached", "silverface", "cyberdeck", "classic", "amber", "vfd", "synthwave"];

const root = document.documentElement;
const video = document.getElementById("demo");
const skinButtons = [...document.querySelectorAll("[data-skin]")];

function setSkin(name, remember) {
  if (!SKINS.includes(name)) name = SKINS[0];
  root.dataset.theme = name;
  for (const b of skinButtons) b.setAttribute("aria-pressed", String(b.dataset.skin === name));
  document.getElementById("skin-name").textContent = name;

  // Swap the recording but stay at the same moment of the demo.
  const src = `media/${name}.mp4`;
  if (video.getAttribute("src") !== src) {
    resume ??= { at: video.currentTime, playing: !video.paused };
    video.poster = `media/${name}.png`;
    video.src = src;
  }
  if (remember) {
    try { localStorage.setItem("cassetty-theme", name); } catch {}
  }
}

// Where to continue once a swapped recording has loaded.
let resume = null;
video.addEventListener("loadedmetadata", () => {
  if (!resume) return;
  video.currentTime = Math.min(resume.at, Math.max(0, video.duration - 0.1));
  if (resume.playing) video.play().catch(() => {});
  resume = null;
});

for (const b of skinButtons) b.addEventListener("click", () => setSkin(b.dataset.skin, true));

addEventListener("keydown", (e) => {
  if (e.key !== "t" || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.target instanceof Element && e.target.closest("input, textarea, [contenteditable]")) return;
  setSkin(SKINS[(SKINS.indexOf(root.dataset.theme) + 1) % SKINS.length], true);
});

if (matchMedia("(prefers-reduced-motion: reduce)").matches) {
  video.removeAttribute("autoplay");
  video.pause();
  video.controls = true;
}

setSkin(root.dataset.theme, false);

// Light up the download key for this machine.
(async () => {
  const ua = navigator.userAgent;
  const mac = /Macintosh/.test(ua) && navigator.maxTouchPoints < 2;
  const linux = /Linux/.test(ua) && !/Android/.test(ua);
  if (!mac && !linux) return;
  // Safari reports Intel on every Mac, so guess Apple Silicon unless told otherwise.
  let arch = mac ? "arm64" : /aarch64|arm64/i.test(ua) ? "arm64" : "amd64";
  try {
    const hints = await navigator.userAgentData?.getHighEntropyValues(["architecture"]);
    if (hints?.architecture) arch = hints.architecture.startsWith("arm") ? "arm64" : "amd64";
  } catch {}
  document.querySelector(`.key[data-os="${mac ? "darwin" : "linux"}"][data-arch="${arch}"]`)?.classList.add("mine");
})();

fetch(`https://api.github.com/repos/${REPO}/releases/latest`)
  .then((r) => (r.ok ? r.json() : null))
  .then((rel) => {
    if (rel?.tag_name) document.getElementById("version").textContent = rel.tag_name;
  })
  .catch(() => {});

for (const b of document.querySelectorAll("[data-copy]")) {
  b.addEventListener("click", async () => {
    const text = document.getElementById(b.dataset.copy).textContent
      .split("\n")
      .map((line) => line.replace(/^\$ /, ""))
      .join("\n");
    try {
      await navigator.clipboard.writeText(text);
      b.textContent = "COPIED";
    } catch {
      b.textContent = "SELECT & COPY";
    }
    setTimeout(() => (b.textContent = "COPY"), 1600);
  });
}
