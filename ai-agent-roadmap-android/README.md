# AI Agent Roadmap Android

[English](README.md) | [简体中文](README.zh-CN.md)

This directory contains a lightweight Android WebView wrapper for the bilingual
research site. Web assets are copied into the APK at build time, so the topic
index, paper visuals, local paper analysis, and browser notes remain available
offline. The site defaults to English and retains the user's language choice.

## Requirements

- JDK 17
- Android SDK Platform 35
- Android Build Tools 35.0.0
- `ANDROID_SDK_ROOT` or `ANDROID_HOME`
- `JAVA_HOME`

## Sync Web Assets

From the repository root:

```bash
npm run android:sync
```

## Build the Debug APK

```bash
npm run android:build
```

The artifact is written to
`ai-agent-roadmap-android/dist/ai-agent-roadmap-debug.apk`. The build script uses
the default Android debug keystore; set `ANDROID_DEBUG_KEYSTORE` to use another
debug signing file.

## Device Regression

`scripts/regression-android.mjs` creates or reuses an Android 35 emulator and
checks the home, topic, paper analysis, architecture, reference implementation,
and language-switch views. It writes screenshots and a JSON report. The full
Android SDK system image is only required for device regression; ordinary
content changes can run the root `npm test` gate.
