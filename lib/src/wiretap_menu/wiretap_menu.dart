// ignore_for_file: invalid_use_of_visible_for_testing_member, invalid_use_of_protected_member

import 'dart:io';

import 'package:code_scout/src/wiretap_menu/controller.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

class WiretapMenu extends StatefulWidget {
  const WiretapMenu({super.key});

  @override
  // ignore: library_private_types_in_public_api
  _WiretapMenuState createState() => _WiretapMenuState();
}

class _WiretapMenuState extends State<WiretapMenu> {
  final _formKey = GlobalKey<FormState>();
  final ipController = TextEditingController();
  final portController = TextEditingController();
  final identifierController = TextEditingController();

  final WireTapMenuController wireTapMenuController = WireTapMenuController();

  Socket? socket;

  @override
  void dispose() {
    // Clean up the controller when the widget is disposed.
    ipController.dispose();
    portController.dispose();
    identifierController.dispose();
    super.dispose();
  }

  @override
  void initState() {
    wireTapMenuController.init();
    super.initState();
  }

  @override
  Widget build(BuildContext context) {
    return ChangeNotifierProvider.value(
      value: wireTapMenuController,
      child: Consumer<WireTapMenuController>(
        builder: (context, config, _) {
          if (config.connected) {
            return Container(
              height: MediaQuery.of(context).size.height * 0.4,
              color: Colors.black,
              padding: const EdgeInsets.all(20),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.start,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Center(
                      child: Text(
                        'Connection Established!!',
                        style: TextStyle(
                          color: Colors.green,
                        ),
                      ),
                    ),
                    const SizedBox(height: 10),
                    Row(
                      children: [
                        const Text(
                          'IP :',
                          style: TextStyle(
                            color: Colors.white,
                          ),
                        ),
                        Text(
                          config.connectedIP ?? '-',
                          style: const TextStyle(
                            color: Colors.yellow,
                          ),
                        ),
                      ],
                    ),
                    Row(
                      children: [
                        const Text(
                          'Port :',
                          style: TextStyle(
                            color: Colors.white,
                          ),
                        ),
                        Text(
                          config.connectedPort.toString(),
                          style: const TextStyle(
                            color: Colors.yellow,
                          ),
                        ),
                      ],
                    ),

                    // Analytics
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.analyticsLogs,
                      title: const Text(
                        "Analytics Logs",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.analyticsLogs =
                            value;
                        config.notifyListeners();
                      },
                    ),

                    // Crash logs
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.crashLogs,
                      title: const Text(
                        "Crash Logs",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.crashLogs = value;
                        config.notifyListeners();
                      },
                    ),

                    // Dev logs
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.devLogs,
                      title: const Text(
                        "Dev Logs",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.devLogs = value;
                        config.notifyListeners();
                      },
                    ),

                    // Dev Traces
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.devTraces,
                      title: const Text(
                        "Dev Traces",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.devTraces = value;
                        config.notifyListeners();
                      },
                    ),

                    // Error Logs
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.errorLogs,
                      title: const Text(
                        "Error Logs",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.errorLogs = value;
                        config.notifyListeners();
                      },
                    ),

                    // Network Calls
                    CheckboxListTile(
                      value: config.codeScoutLoggingConfiguration.networkCall,
                      title: const Text(
                        "Network Calls",
                        style: TextStyle(color: Colors.white),
                      ),
                      onChanged: (value) {
                        if (value == null) return;
                        config.codeScoutLoggingConfiguration.networkCall =
                            value;
                        config.notifyListeners();
                      },
                    ),

                    Center(
                      child: MaterialButton(
                        onPressed: () {
                          config.resetConnection();
                        },
                        color: Colors.red,
                        child: const Text(
                          'Disconnect',
                          style: TextStyle(
                            color: Colors.white,
                          ),
                        ),
                      ),
                    )
                  ],
                ),
              ),
            );
          }
          return _form();
        },
      ),
    );
  }

  Form _form() {
    return Form(
      key: _formKey,
      child: GestureDetector(
        onTap: () {
          FocusScope.of(context).unfocus();
        },
        child: Padding(
          padding: const EdgeInsets.all(16.0),
          child: Column(
            children: <Widget>[
              TextFormField(
                controller: ipController,
                // keyboardType: TextInputType.number,
                decoration: const InputDecoration(labelText: 'IP Address'),
                validator: (value) {
                  // Add your own validation logic here (e.g., check if it's a valid IP)
                  if (value == null || value.isEmpty) {
                    return 'Please enter an IP address';
                  }
                  return null;
                },
              ),
              TextFormField(
                controller: portController,
                decoration: const InputDecoration(labelText: 'Port'),
                validator: (value) {
                  if (value == null || value.isEmpty) {
                    return 'Please enter a port number';
                  }
                  return null;
                },
                //keyboardType: TextInputType.number,
              ),
              TextFormField(
                controller: identifierController,
                decoration: const InputDecoration(labelText: 'Identifier'),
                validator: (value) {
                  if (value == null || value.isEmpty) {
                    return 'Please enter an identifier';
                  }
                  return null;
                },
                // keyboardType: TextInputType.number,
              ),
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 16.0),
                child: Row(
                  children: [
                    ElevatedButton(
                      onPressed: () {
                        if (_formKey.currentState?.validate() == false) return;

                        wireTapMenuController.tryToConnect(
                          ip: ipController.text,
                          port: portController.text,
                          identifier: identifierController.text,
                          notifier: null,
                        );
                      },
                      child: const Text('Connect'),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
