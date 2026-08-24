# AI Agent Roadmap Android

[English](README.md) | [简体中文](README.zh-CN.md)

这是双语研究图谱网站的轻量 Android WebView 封装。网页资源在构建时复制到 APK，因此主题索引、论文概念图、论文分析和本地笔记可以离线使用。站点默认英语，并保留用户的语言选择。

## 环境

- JDK 17
- Android SDK Platform 35
- Android Build Tools 35.0.0
- `ANDROID_SDK_ROOT` 或 `ANDROID_HOME`
- `JAVA_HOME`

## 同步网页资源

从仓库根目录运行：

```bash
npm run android:sync
```

## 构建 Debug APK

```bash
npm run android:build
```

产物位于 `ai-agent-roadmap-android/dist/ai-agent-roadmap-debug.apk`。构建脚本使用默认 Android debug keystore，也可以通过 `ANDROID_DEBUG_KEYSTORE` 指定其他调试签名文件。

## 设备回归

`scripts/regression-android.mjs` 会创建或复用 Android 35 模拟器，验证首页、主题、论文分析、架构和参考实现页，并输出截图与 JSON 报告。该检查需要完整 Android SDK system image，普通内容贡献只需运行根目录的 `npm test`。
