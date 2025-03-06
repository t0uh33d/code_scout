import 'package:flutter/material.dart';

class InspectorController {
  static final InspectorController _singleton = InspectorController._internal();

  factory InspectorController() {
    return _singleton;
  }

  InspectorController._internal();

  static ValueNotifier<bool> isInspectorVisible = ValueNotifier(false);

  static void toggleInspector() {
    isInspectorVisible.value = !isInspectorVisible.value;
  }

  static void hideInspector() {
    isInspectorVisible.value = false;
  }
}
