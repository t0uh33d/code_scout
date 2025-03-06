import 'package:code_scout/code_scout.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

class TriggerLogs extends StatefulWidget {
  const TriggerLogs({super.key});

  @override
  State<TriggerLogs> createState() => _TriggerLogsState();
}

class _TriggerLogsState extends State<TriggerLogs> {
  @override
  void initState() {
    CodeScout.init(
      terimalLoggingConfigutation: CodeScoutLoggingConfiguration(
        isDebugMode: kDebugMode,
        analyticsLogs: true,
        crashLogs: true,
        devLogs: true,
        devTraces: true,
        errorLogs: true,
        networkCall: true,
      ),
    );
    super.initState();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Trigger Logs')),
      body: Column(
        children: [
          const Text('Trigger Logs'),
          ElevatedButton(
            onPressed: () {
              CodeScout.logAnalytics(
                'Analytics Triggered',
                dateTime: DateTime.now(),
              );
            },
            child: const Text('Analytics Logs'),
          ),
        ],
      ),
    );
  }
}
