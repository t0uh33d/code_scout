import 'package:code_scout/code_scout.dart' show Wiretap;
import 'package:code_scout/src/wiretap_menu/wiretap_menu.dart' show WiretapMenu;
import 'package:flutter/material.dart';

import '../utils/draggable_widget.dart';

class OverlayManager {
  static final OverlayManager _singleton = OverlayManager._internal();

  factory OverlayManager() {
    return _singleton;
  }

  OverlayManager._internal();

  final _entries = <OverlayEntry>[];
  bool isBottomSheetVisible = false;
  BuildContext? context;
  Widget? overlayChild;

  static OverlayState? of(BuildContext context) {
    return Overlay.of(context);
  }

  OverlayEntry createOverlayEntry() {
    final entry = OverlayEntry(
      builder: (context) => DraggableFloatingWindow(
        onTap: () {
          BuildContext? freshContext = Wiretap.fetcher?.call();
          if (isBottomSheetVisible) {
            isBottomSheetVisible = false;
            if (Navigator.of(freshContext ?? context).canPop()) {
              Navigator.pop(freshContext ?? context, true);
            }
          } else {
            isBottomSheetVisible = true;
            _bottomSheet(freshContext ?? context);
          }
        },
        child: Container(
          // color: Colors.red,
          constraints: const BoxConstraints(
            maxHeight: 80,
            maxWidth: 80,
          ),
          child: overlayChild ??
              Image.asset(
                'assets/cwa_setting.png',
                package: 'flutter_wiretap',
                height: 80,
                width: 80,
                fit: BoxFit.cover,
              ),
        ),
      ),
    );

    insertOverlay(entry);
    return entry;
  }

  Future<dynamic> _bottomSheet(BuildContext context) {
    return showModalBottomSheet(
      // useRootNavigator: true,
      enableDrag: false,
      context: context,
      showDragHandle: false,
      isDismissible: false,
      builder: (context) {
        return const WiretapMenu();
      },
    );
  }

  void insertOverlay(OverlayEntry entry) {
    Future.delayed(const Duration(seconds: 0), () {
      if (_entries.isEmpty) {
        _entries.add(entry);
      }
      Overlay.of(context!).insert(entry);
    });
  }

  // Remove an existing overlay
  void removeOverlay() {
    if (_entries.isNotEmpty) {
      final lastEntry = _entries.removeLast();
      lastEntry.remove();
    }
    return;
  }
}
