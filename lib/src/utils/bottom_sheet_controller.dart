import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart'; // Add this line

class BottomSheetController {
  static final BottomSheetController _instance =
      BottomSheetController._internal();
  factory BottomSheetController() {
    return _instance;
  }
  BottomSheetController._internal();

  static ValueNotifier<bool> isBottomSheetVisible = ValueNotifier(false);

  static void toggleBottomSheet() {
    isBottomSheetVisible.value = !isBottomSheetVisible.value;
  }

  static void hideBottomSheet() {
    isBottomSheetVisible.value = false;
  }

  static void showBottomSheet() {
    isBottomSheetVisible.value = true;
  }
}
