// ignore_for_file: public_member_api_docs, sort_constructors_first, non_constant_identifier_names
import 'dart:convert';

import 'package:logger/logger.dart';

part 'code_scout_comms.dart';

// Define a type for the CodeScoutSocketLogger function
typedef CodeScoutSocketLogger = void Function(
  bool Function(CodeScoutLoggingConfiguration configuration) shouldLog,
  OutputEvent? outputEvent,
);

// Define the CodeScout class
class CodeScout {
  // Create a singleton instance of CodeScout
  static final CodeScout i = CodeScout._i();

  CodeScout._i();

  // Define a factory constructor that returns the singleton instance
  factory CodeScout() => i;

  // Define a late-initialized static variable for logging configuration
  static late CodeScoutLoggingConfiguration _terimalLoggingConfigutation;

  // Create an instance of the Logger class from the logger package
  static final Logger _logger = Logger();

  // Define a static variable for the CodeScoutSocketLogger function
  static CodeScoutSocketLogger? _codeScoutSocketLogger;

  // Bind the CodeScoutSocketLogger function to the static variable
  static void bindSocketLogger(CodeScoutSocketLogger codeScoutSocketLogger) {
    _codeScoutSocketLogger = codeScoutSocketLogger;
  }

  // Unbind the CodeScoutSocketLogger function
  static void unbindSocketLogger() {
    _codeScoutSocketLogger = null;
  }

  // Initialize the CodeScout class with logging configuration
  static void init(
      {required CodeScoutLoggingConfiguration terimalLoggingConfigutation}) {
    _terimalLoggingConfigutation = terimalLoggingConfigutation;
  }

  // Log a development trace message
  static void logDevTrace(
    dynamic message, {
    DateTime? dateTime,
    Object? error,
    StackTrace? stackTrace,
  }) {
    // Determine if logging should be avoided based on the logging configuration
    bool avoidLogging = !_terimalLoggingConfigutation.isDebugMode ||
        !_terimalLoggingConfigutation.devTraces;

    // Log the message using the Logger class and get the output event
    OutputEvent? outputEvent = _logger.log(
      Level.trace,
      message,
      time: dateTime ?? DateTime.now(),
      error: error,
      stackTrace: stackTrace,
      onlyReturnOutput: avoidLogging,
    );

    // Call the CodeScoutSocketLogger function with the appropriate configuration and output event
    _codeScoutSocketLogger?.call(
      (socketConfig) => socketConfig.devTraces,
      outputEvent,
    );
  }

  // Log a debug message
  static void logDebug(
    dynamic message, {
    DateTime? dateTime,
    Object? error,
    StackTrace? stackTrace,
  }) {
    // Determine if logging should be avoided based on the logging configuration
    bool avoidLogging = !_terimalLoggingConfigutation.isDebugMode ||
        !_terimalLoggingConfigutation.devLogs;

    // Log the message using the Logger class and get the output event
    OutputEvent? outputEvent = _logger.log(
      Level.debug,
      message,
      time: dateTime ?? DateTime.now(),
      error: error,
      stackTrace: stackTrace,
      onlyReturnOutput: avoidLogging,
    );

    // Call the CodeScoutSocketLogger function with the appropriate configuration and output event
    _codeScoutSocketLogger?.call(
      (socketConfig) => socketConfig.devLogs,
      outputEvent,
    );
  }

  // Log a crash message
  static void logCrash(
    dynamic message, {
    DateTime? dateTime,
    required Object? error,
    required StackTrace? stackTrace,
  }) {
    // Determine if logging should be avoided based on the logging configuration
    bool avoidLogging = !_terimalLoggingConfigutation.isDebugMode ||
        !_terimalLoggingConfigutation.crashLogs;

    // Log the message using the Logger class and get the output event
    OutputEvent? outputEvent = _logger.log(
      Level.fatal,
      message,
      time: dateTime ?? DateTime.now(),
      error: error,
      stackTrace: stackTrace,
      onlyReturnOutput: avoidLogging,
    );

    // Call the CodeScoutSocketLogger function with the appropriate configuration and output event
    _codeScoutSocketLogger?.call(
      (socketConfig) => socketConfig.crashLogs,
      outputEvent,
    );
  }

  // Log an error message
  static void logError(
    dynamic message, {
    DateTime? dateTime,
    Object? error,
    StackTrace? stackTrace,
  }) {
    // Determine if logging should be avoided based on the logging configuration
    bool avoidLogging = !_terimalLoggingConfigutation.isDebugMode ||
        !_terimalLoggingConfigutation.errorLogs;

    // Log the message using the Logger class and get the output event
    OutputEvent? outputEvent = _logger.log(
      Level.error,
      message,
      time: dateTime ?? DateTime.now(),
      error: error,
      stackTrace: stackTrace,
      onlyReturnOutput: avoidLogging,
    );

    // Call the CodeScoutSocketLogger function with the appropriate configuration and output event
    _codeScoutSocketLogger?.call(
      (socketConfig) => socketConfig.errorLogs,
      outputEvent,
    );
  }

  // Log an analytics message
  static void logAnalytics(
    dynamic message, {
    DateTime? dateTime,
    Object? error,
    StackTrace? stackTrace,
  }) {
    // Determine if logging should be avoided based on the logging configuration
    bool avoidLogging = !_terimalLoggingConfigutation.isDebugMode ||
        !_terimalLoggingConfigutation.analyticsLogs;

    // Log the message using the Logger class and get the output event
    OutputEvent? outputEvent = _logger.log(
      Level.warning,
      message,
      time: dateTime ?? DateTime.now(),
      error: error,
      stackTrace: stackTrace,
      onlyReturnOutput: avoidLogging,
    );

    // Call the CodeScoutSocketLogger function with the appropriate configuration and output event
    _codeScoutSocketLogger?.call(
      (socketCofig) => socketCofig.analyticsLogs,
      outputEvent,
    );
  }
}

// Define the CodeScoutLoggingConfiguration class
class CodeScoutLoggingConfiguration {
  bool devTraces;
  bool devLogs;
  bool networkCall;
  bool analyticsLogs;
  bool crashLogs;
  bool errorLogs;
  bool isDebugMode;

  // Define the constructor for CodeScoutLoggingConfiguration with default values
  CodeScoutLoggingConfiguration({
    this.devTraces = false,
    this.devLogs = false,
    this.networkCall = false,
    this.analyticsLogs = false,
    this.crashLogs = false,
    this.errorLogs = false,
    this.isDebugMode = false,
  });
}
