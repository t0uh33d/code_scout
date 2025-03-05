// ignore_for_file: public_member_api_docs, sort_constructors_first
part of 'code_scout.dart';

class CodeScoutCommands {
  static const String establishConnection = 'establish_connection';

  static const String breakConnection = 'break_connection';

  static const String communication = 'communincation';

  static const String connectionApproved = 'connection_approved';
}

class CodeScoutPayloadType {
  static const String devTrace = 'dev_trace';

  static const String devLogs = 'dev_logs';

  static const String networkCall = 'network_call';

  static const String analyticsLogs = 'analytics_logs';

  static const String crashLogs = 'crash_logs';

  static const String errorLogs = 'error_logs';

  static const String identifier = 'identifier';
}

class CodeScoutComms {
  final String command;
  final String payloadType;
  final Map<String, dynamic> data;
  CodeScoutComms({
    required this.command,
    required this.payloadType,
    required this.data,
  });

  Map<String, dynamic> toMap() {
    return <String, dynamic>{
      'command': command,
      'payloadType': payloadType,
      'data': data,
    };
  }

  factory CodeScoutComms.fromMap(Map<String, dynamic> map) {
    return CodeScoutComms(
      command: map['command'] as String,
      payloadType: map['payloadType'] as String,
      data: Map<String, dynamic>.from((map['data'] as Map<String, dynamic>)),
    );
  }

  String toJson() => json.encode(toMap());

  factory CodeScoutComms.fromJson(String source) =>
      CodeScoutComms.fromMap(json.decode(source) as Map<String, dynamic>);

  @override
  String toString() => toJson();
}
