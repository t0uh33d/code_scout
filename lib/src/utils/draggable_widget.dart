import 'package:flutter/material.dart';

class DraggableFloatingWindow extends StatefulWidget {
  const DraggableFloatingWindow(
      {super.key, required this.child, required this.onTap});
  final Widget child;
  final void Function() onTap;
  @override
  State<DraggableFloatingWindow> createState() =>
      _DraggableFloatingWindowState();
}

class _DraggableFloatingWindowState extends State<DraggableFloatingWindow> {
  Offset? position;
  @override
  Widget build(BuildContext context) {
    return Positioned(
      left: position?.dx ?? 0,
      top: position?.dy ?? 100,
      child: GestureDetector(
        onPanStart: (details) => setState(
            () => position = details.globalPosition - const Offset(25, 25)),
        onPanUpdate: (details) => setState(() {
          position = details.globalPosition - const Offset(25, 25);
        }),
        onTap: widget.onTap,
        child: widget.child,
      ),
    );
  }
}
