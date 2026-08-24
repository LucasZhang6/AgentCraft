#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
if [[ -z "$SDK" ]]; then
  echo "Set ANDROID_SDK_ROOT or ANDROID_HOME before building." >&2
  exit 1
fi
if [[ -z "${JAVA_HOME:-}" ]]; then
  if [[ -x /usr/libexec/java_home ]]; then
    JAVA_HOME="$(/usr/libexec/java_home -v 17)"
  else
    echo "Set JAVA_HOME to a JDK 17 installation before building." >&2
    exit 1
  fi
fi
BUILD_TOOLS="$SDK/build-tools/35.0.0"
ANDROID_JAR="$SDK/platforms/android-35/android.jar"
PACKAGE="com.airoadmap.agent"
BUILD_DIR="$ROOT/build"
DIST_DIR="$ROOT/dist"
GEN_DIR="$BUILD_DIR/generated"
CLASS_DIR="$BUILD_DIR/classes"
DEX_DIR="$BUILD_DIR/dex"
KEYSTORE="${ANDROID_DEBUG_KEYSTORE:-$HOME/.android/debug.keystore}"
APK_OUT="$DIST_DIR/ai-agent-roadmap-debug.apk"

export JAVA_HOME
export PATH="$JAVA_HOME/bin:$PATH"

node "$ROOT/scripts/sync-web-assets.mjs"

rm -rf "$BUILD_DIR" "$DIST_DIR"
mkdir -p "$BUILD_DIR" "$DIST_DIR" "$GEN_DIR" "$CLASS_DIR" "$DEX_DIR"

"$BUILD_TOOLS/aapt2" compile --dir "$ROOT/src/main/res" -o "$BUILD_DIR/resources.zip"
"$BUILD_TOOLS/aapt2" link \
  -o "$BUILD_DIR/base.apk" \
  -I "$ANDROID_JAR" \
  --manifest "$ROOT/src/main/AndroidManifest.xml" \
  --java "$GEN_DIR" \
  --min-sdk-version 23 \
  --target-sdk-version 35 \
  -A "$ROOT/src/main/assets" \
  "$BUILD_DIR/resources.zip"

"$JAVA_HOME/bin/javac" \
  -source 8 \
  -target 8 \
  -bootclasspath "$ANDROID_JAR" \
  -classpath "$ANDROID_JAR" \
  -d "$CLASS_DIR" \
  "$GEN_DIR/com/airoadmap/agent/R.java" \
  "$ROOT/src/main/java/com/airoadmap/agent/MainActivity.java"

"$BUILD_TOOLS/d8" \
  --lib "$ANDROID_JAR" \
  --output "$DEX_DIR" \
  $(find "$CLASS_DIR" -name "*.class")

(cd "$DEX_DIR" && zip -q "$BUILD_DIR/base.apk" classes.dex)
"$BUILD_TOOLS/zipalign" -f -p 4 "$BUILD_DIR/base.apk" "$BUILD_DIR/aligned.apk"
"$BUILD_TOOLS/apksigner" sign \
  --ks "$KEYSTORE" \
  --ks-pass pass:android \
  --key-pass pass:android \
  --out "$APK_OUT" \
  "$BUILD_DIR/aligned.apk"
"$BUILD_TOOLS/apksigner" verify --verbose --print-certs "$APK_OUT"

echo "Built $APK_OUT"
