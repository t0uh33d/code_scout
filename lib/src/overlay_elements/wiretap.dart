import 'package:code_scout/src/overlay_elements/overlay_manager.dart'
    show OverlayManager;
import 'package:flutter/material.dart';

final OverlayManager overlayManager = OverlayManager();

typedef FreshContextFetcher = BuildContext Function();

class Wiretap {
  static FreshContextFetcher? fetcher;
  static void initialize({
    Widget? overlayChild,
    required BuildContext context,
    FreshContextFetcher? freshContextFetcher,
  }) {
    fetcher = freshContextFetcher;

    if (overlayManager.context == null) {
      if (overlayChild != null) overlayManager.overlayChild = overlayChild;
      overlayManager.context = context;
      overlayManager.removeOverlay();
      overlayManager.createOverlayEntry();
    }
  }
}
