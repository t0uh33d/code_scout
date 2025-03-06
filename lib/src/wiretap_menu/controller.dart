import 'dart:convert';
import 'dart:io';

import 'package:code_scout/code_scout.dart';
import 'package:flutter/material.dart';

class WireTapMenuController extends ChangeNotifier {
  static final WireTapMenuController i = WireTapMenuController._i();

  factory WireTapMenuController() => i;

  WireTapMenuController._i();

  Socket? socket;
  String? connectedIP;
  int? connectedPort;

  bool connected = false;

  final CodeScoutLoggingConfiguration codeScoutLoggingConfiguration =
      CodeScoutLoggingConfiguration(
    devLogs: true,
  );

  void init() {
    CodeScout.bindSocketLogger((shouldLog, outputEvent) {
      if (connected &&
          shouldLog.call(codeScoutLoggingConfiguration) &&
          outputEvent != null) {
        String data = outputEvent.lines.join('\n');

        CodeScoutComms codeScoutComms = CodeScoutComms(
          command: CodeScoutCommands.communication,
          payloadType: CodeScoutPayloadType.devTrace,
          data: {
            "output": utf8.encode(data),
          },
        );
        socket?.write(codeScoutComms);
      }
    });
  }

  void tryToConnect({
    required String ip,
    required String port,
    required String identifier,
    required ValueNotifier<bool>? notifier,
  }) async {
    notifier?.value = true;
    try {
      int port0 = int.parse(port);
      Socket socket0 = await Socket.connect(ip, port0);

      CodeScoutComms connectionComms = CodeScoutComms(
        command: CodeScoutCommands.establishConnection,
        payloadType: CodeScoutPayloadType.identifier,
        data: {
          CodeScoutPayloadType.identifier: identifier,
        },
      );

      socket0.listen(
        (event) {
          CodeScoutComms codeScoutComms;
          try {
            String eventData = String.fromCharCodes(event);
            codeScoutComms = CodeScoutComms.fromJson(eventData);
          } catch (e) {
            CodeScout.logError("Failed to connect", error: e);
            return;
          }

          if (codeScoutComms.command == CodeScoutCommands.connectionApproved) {
            socket = socket0;
            connected = true;
            connectedIP = ip;
            connectedPort = port0;
            notifyListeners();
            return;
          }

          throw codeScoutComms;
        },
        // cancelOnError: true,
        // onError: resetConnection,
        onDone: resetConnection,
      );

      socket0.write(connectionComms);
    } catch (e) {
      CodeScout.logError(
        "Failed to connect to socket in {tryToConnect}",
        error: e,
      );
    } finally {
      notifier?.value = false;
    }
  }

  void resetConnection() {
    try {
      socket?.flush();
      socket?.close();
    } catch (e) {
      CodeScout.logDevTrace("Socket is already closed", error: e);
    }

    socket = null;
    connected = false;
    connectedIP = null;
    connectedPort = null;
    notifyListeners();
  }
}
