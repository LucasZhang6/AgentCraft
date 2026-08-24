import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const sdk = process.env.ANDROID_SDK_ROOT || process.env.ANDROID_HOME;
const javaHome = process.env.JAVA_HOME;
if (!sdk || !javaHome) {
  throw new Error("Set ANDROID_SDK_ROOT (or ANDROID_HOME) and JAVA_HOME before Android regression.");
}
const adb = path.join(sdk, "platform-tools/adb");
const emulator = path.join(sdk, "emulator/emulator");
const avdmanager = path.join(sdk, "cmdline-tools/latest/bin/avdmanager");
const apk = path.join(root, "dist/ai-agent-roadmap-debug.apk");
const packageName = "com.airoadmap.agent";
const activityName = `${packageName}/.MainActivity`;
const avdName = process.env.AVD_NAME || "roadmap_api35";
const systemImage = "system-images;android-35;google_apis;arm64-v8a";
const distDir = path.join(root, "dist");
const avdHome = path.join(root, "build/android-avd");
const androidEnv = {
  ...process.env,
  ANDROID_HOME: sdk,
  ANDROID_SDK_ROOT: sdk,
  ANDROID_AVD_HOME: avdHome,
  JAVA_HOME: javaHome,
  PATH: `${path.join(javaHome, "bin")}:${process.env.PATH || ""}`
};

function run(cmd, args, options = {}) {
  return execFileSync(cmd, args, {
    encoding: options.encoding ?? "utf8",
    maxBuffer: options.maxBuffer ?? 64 * 1024 * 1024,
    stdio: options.stdio ?? ["ignore", "pipe", "pipe"],
    input: options.input,
    env: androidEnv
  });
}

function adbRun(args, options = {}) {
  return run(adb, args, options);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function listBootedDevices() {
  const output = adbRun(["devices"]).trim().split("\n").slice(1);
  return output
    .map((line) => line.trim().split(/\s+/))
    .filter(([serial, state]) => serial && state === "device")
    .map(([serial]) => serial);
}

function ensureAvd() {
  mkdirSync(avdHome, { recursive: true });
  const existing = run(avdmanager, ["list", "avd"]);
  if (existing.includes(`Name: ${avdName}`)) {
    return;
  }
  run(
    avdmanager,
    ["create", "avd", "-n", avdName, "-k", systemImage, "-d", "medium_phone", "--force"],
    { input: "no\n" }
  );
}

async function ensureDevice() {
  ensureAvd();
  const existing = listBootedDevices();
  if (existing.length) {
    return { serial: existing[0], started: false };
  }

  const child = spawn(
    emulator,
    [
      "-avd",
      avdName,
      "-no-window",
      "-no-audio",
      "-no-boot-anim",
      "-no-snapshot-load",
      "-no-snapshot-save"
    ],
    {
      detached: true,
      stdio: "ignore",
      env: androidEnv
    }
  );
  child.unref();

  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    await sleep(2500);
    const devices = listBootedDevices();
    if (!devices.length) continue;
    const serial = devices[0];
    try {
      const booted = adbRun(["-s", serial, "shell", "getprop", "sys.boot_completed"]).trim();
      if (booted === "1") {
        return { serial, started: true };
      }
    } catch {
      // The emulator can briefly accept adb while Android is still starting.
    }
  }
  throw new Error(`Timed out waiting for AVD ${avdName}`);
}

function latestProbePayload(logcatOutput) {
  const lines = logcatOutput
    .split("\n")
    .filter((line) => line.includes("AIAgentRoadmapProbe"));
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    const jsonStart = lines[i].indexOf("{");
    if (jsonStart < 0) continue;
    try {
      return JSON.parse(lines[i].slice(jsonStart));
    } catch {
      // Skip partial log lines.
    }
  }
  return null;
}

async function waitForProbe(serial, route, expect) {
  adbRun(["-s", serial, "logcat", "-c"]);
  adbRun(["-s", serial, "shell", "am", "force-stop", packageName]);
  await sleep(300);
  adbRun(["-s", serial, "shell", "am", "start", "-W", "-n", activityName, "--es", "route", route]);

  const deadline = Date.now() + 20000;
  let last = null;
  while (Date.now() < deadline) {
    await sleep(1200);
    const output = adbRun(["-s", serial, "logcat", "-d", "-s", "AIAgentRoadmapProbe:I", "*:S"]);
    last = latestProbePayload(output);
    if (last && expect(last)) {
      return last;
    }
  }
  throw new Error(`Regression probe failed for ${route}; last payload: ${JSON.stringify(last)}`);
}

async function main() {
  mkdirSync(distDir, { recursive: true });
  const { serial, started } = await ensureDevice();
  console.log(`Using emulator/device ${serial}`);

  try {
    adbRun(["-s", serial, "install", "-r", apk], { stdio: ["ignore", "pipe", "inherit"] });

    const checks = [
      {
        route: "/",
        expect: (payload) =>
          payload.hash === "#/" &&
          payload.lang === "en" &&
          payload.languageControls === 1 &&
          payload.moduleCards === 8 &&
          payload.bodyChars > 1000
      },
      {
        route: "/module/llm-foundation",
        expect: (payload) =>
          payload.hash === "#/module/llm-foundation" &&
          payload.paperCards === 6 &&
          payload.images === 6 &&
          payload.loadedImages === 6 &&
          payload.brokenImages.length === 0
      },
      {
        route: "/paper/llm-foundation/0",
        expect: (payload) =>
          payload.hash === "#/paper/llm-foundation/0" &&
          payload.title.includes("Attention") &&
          payload.paperReader === true &&
          payload.notesPanel === true &&
          payload.images === 1 &&
          payload.loadedImages === 1 &&
          payload.brokenImages.length === 0
      },
      {
        route: "/architecture",
        expect: (payload) =>
          payload.hash === "#/architecture" &&
          payload.title.includes("Personal Agent") &&
          payload.bodyChars > 600
      },
      {
        route: "/practice/timeline",
        expect: (payload) =>
          payload.hash === "#/practice/timeline" &&
          payload.title.includes("Paper Agent") &&
          payload.bodyChars > 2500
      }
    ];

    const results = [];
    for (const check of checks) {
      const result = await waitForProbe(serial, check.route, check.expect);
      results.push(result);
      console.log(`PASS ${check.route}: ${JSON.stringify(result)}`);
    }

    adbRun(["-s", serial, "shell", "am", "force-stop", packageName]);
    await sleep(300);
    adbRun(["-s", serial, "shell", "am", "start", "-W", "-n", activityName, "--es", "route", "/paper/llm-foundation/0"]);
    await sleep(2500);
    const screenshot = adbRun(["-s", serial, "exec-out", "screencap", "-p"], {
      encoding: "buffer",
      maxBuffer: 64 * 1024 * 1024
    });
    writeFileSync(path.join(distDir, "regression-paper-reader.png"), screenshot);
    writeFileSync(path.join(distDir, "regression-report.json"), JSON.stringify(results, null, 2));
  } finally {
    if (started) {
      try {
        adbRun(["-s", serial, "emu", "kill"], { stdio: "ignore" });
      } catch {
        // Ignore shutdown races.
      }
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
