package com.airoadmap.agent;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Intent;
import android.graphics.Color;
import android.net.Uri;
import android.os.Bundle;
import android.util.Log;
import android.view.ViewGroup;
import android.view.Window;
import android.webkit.JavascriptInterface;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;

public class MainActivity extends Activity {
    private static final String TAG = "AIAgentRoadmapProbe";
    private static final String ASSET_URL = "file:///android_asset/index.html";
    private WebView webView;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        configureWindow();
        webView = new WebView(this);
        setContentView(
                webView,
                new ViewGroup.LayoutParams(
                        ViewGroup.LayoutParams.MATCH_PARENT,
                        ViewGroup.LayoutParams.MATCH_PARENT));
        configureWebView(webView);
        loadRouteFromIntent(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        loadRouteFromIntent(intent);
    }

    private void configureWindow() {
        Window window = getWindow();
        window.setStatusBarColor(Color.rgb(248, 250, 252));
        window.setNavigationBarColor(Color.WHITE);
    }

    @SuppressLint({"SetJavaScriptEnabled", "AddJavascriptInterface"})
    private void configureWebView(WebView view) {
        WebView.setWebContentsDebuggingEnabled(true);
        WebSettings settings = view.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setDatabaseEnabled(true);
        settings.setAllowFileAccess(true);
        settings.setAllowContentAccess(true);
        settings.setAllowFileAccessFromFileURLs(true);
        settings.setAllowUniversalAccessFromFileURLs(true);
        settings.setLoadWithOverviewMode(false);
        settings.setUseWideViewPort(true);
        settings.setTextZoom(100);
        view.addJavascriptInterface(new ProbeBridge(), "AndroidProbe");
        view.setWebViewClient(new RoadmapWebViewClient());
    }

    private void loadRouteFromIntent(Intent intent) {
        String route = "/";
        if (intent != null) {
            String extra = intent.getStringExtra("route");
            if (extra != null && !extra.trim().isEmpty()) {
                route = extra.trim();
            }
        }
        if (!route.startsWith("/")) {
            route = "/" + route;
        }
        webView.loadUrl(ASSET_URL + "#" + route);
    }

    @Override
    public void onBackPressed() {
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
            return;
        }
        super.onBackPressed();
    }

    public final class ProbeBridge {
        @JavascriptInterface
        public void report(String payload) {
            Log.i(TAG, payload);
        }
    }

    private final class RoadmapWebViewClient extends WebViewClient {
        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            return handleExternalUrl(request.getUrl());
        }

        @Override
        public boolean shouldOverrideUrlLoading(WebView view, String url) {
            return handleExternalUrl(Uri.parse(url));
        }

        private boolean handleExternalUrl(Uri uri) {
            if (uri == null) {
                return false;
            }
            String scheme = uri.getScheme();
            if ("http".equalsIgnoreCase(scheme) || "https".equalsIgnoreCase(scheme)) {
                Intent intent = new Intent(Intent.ACTION_VIEW, uri);
                startActivity(intent);
                return true;
            }
            return false;
        }
    }
}
