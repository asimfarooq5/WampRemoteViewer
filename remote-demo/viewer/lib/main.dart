import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:xconn_dart/xconn_dart.dart';

void main() {
  runApp(const RemoteViewerApp());
}

class RemoteViewerApp extends StatelessWidget {
  const RemoteViewerApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(
      debugShowCheckedModeBanner: false,
      home: RemoteViewerPage(),
    );
  }
}

class RemoteViewerPage extends StatefulWidget {
  const RemoteViewerPage({super.key});

  @override
  State<RemoteViewerPage> createState() => _RemoteViewerPageState();
}

class _RemoteViewerPageState extends State<RemoteViewerPage> {
  late final XConnClient _client;
  Uint8List? _lastFrame;
  String _status = 'connecting';
  final FocusNode _focusNode = FocusNode();

  @override
  void initState() {
    super.initState();
    _connect();
  }

  Future<void> _connect() async {
    _client = XConnClient(
      url: const String.fromEnvironment('WAMP_URL', defaultValue: 'ws://localhost:8080/ws'),
      realm: const String.fromEnvironment('WAMP_REALM', defaultValue: 'default'),
    );

    await _client.connect();
    await _client.call('io.xconn.desktop.start');

    _client.subscribe('io.xconn.desktop.frame', (event) {
      final bytes = event.args.first as List<int>;
      setState(() {
        _lastFrame = Uint8List.fromList(bytes);
        _status = 'streaming';
      });
    });

    setState(() {
      _status = 'connected';
    });
  }

  Future<void> _sendMouse(Offset pos, Size size) async {
    final x = pos.dx.round();
    final y = pos.dy.round();
    await _client.call('io.xconn.desktop.mouse', args: [x, y, 'left']);
  }

  Future<void> _sendKey(String key) {
    return _client.call('io.xconn.desktop.key', args: [key]);
  }

  @override
  void dispose() {
    _client.call('io.xconn.desktop.stop');
    _client.close();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Remote Viewer ($_status)')),
      body: KeyboardListener(
        focusNode: _focusNode,
        autofocus: true,
        onKeyEvent: (event) {
          if (event is KeyDownEvent) {
            _sendKey(event.logicalKey.keyLabel.toLowerCase());
          }
        },
        child: LayoutBuilder(
          builder: (context, constraints) {
            final frame = _lastFrame;
            return GestureDetector(
              onTapDown: (details) => _sendMouse(details.localPosition, constraints.biggest),
              child: Container(
                color: Colors.black,
                alignment: Alignment.center,
                child: frame == null
                    ? const CircularProgressIndicator()
                    : Image.memory(frame, gaplessPlayback: true, fit: BoxFit.contain),
              ),
            );
          },
        ),
      ),
    );
  }
}
