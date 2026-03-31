import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

const _apiBaseUrl = 'https://vual.up.railway.app';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final session = await SessionStore.load();
  runApp(VualApp(session: session));
}

class VualApp extends StatelessWidget {
  const VualApp({super.key, required this.session});

  final SessionStore session;

  @override
  Widget build(BuildContext context) {
    final base = ThemeData.dark(useMaterial3: true);
    final textTheme = base.textTheme.apply(
      bodyColor: AppColors.textPrimary,
      displayColor: AppColors.textPrimary,
    );

    return MaterialApp(
      title: 'Vual',
      debugShowCheckedModeBanner: false,
      theme: base.copyWith(
        scaffoldBackgroundColor: AppColors.bgBase,
        textTheme: textTheme,
        colorScheme: base.colorScheme.copyWith(
          primary: AppColors.accent,
          secondary: AppColors.green,
          surface: AppColors.bgSurface,
        ),
      ),
      home: RootScreen(session: session),
    );
  }
}

class RootScreen extends StatefulWidget {
  const RootScreen({super.key, required this.session});

  final SessionStore session;

  @override
  State<RootScreen> createState() => _RootScreenState();
}

class _RootScreenState extends State<RootScreen> {
  late final ApiClient _api;

  @override
  void initState() {
    super.initState();
    _api = ApiClient(baseUrl: _apiBaseUrl, session: widget.session);
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.session,
      builder: (context, _) {
        if (!widget.session.isAuthorized) {
          return AuthScreen(api: _api, session: widget.session);
        }
        return ChatListScreen(api: _api, session: widget.session);
      },
    );
  }
}

class AppColors {
  static const bgBase = Color(0xFF0E1117);
  static const bgSurface = Color(0xFF161B24);
  static const bgElevated = Color(0xFF1C2333);
  static const bgHover = Color(0xFF222B3A);
  static const bgInput = Color(0xFF1A2030);
  static const accent = Color(0xFF3B82F6);
  static const green = Color(0xFF22C55E);
  static const red = Color(0xFFEF4444);
  static const textPrimary = Color(0xFFE8EDF5);
  static const textSecondary = Color(0xFF7A8899);
  static const textMuted = Color(0xFF4A5568);
}

TextStyle monoStyle({
  double fontSize = 11,
  Color color = AppColors.textSecondary,
  FontWeight fontWeight = FontWeight.w400,
}) {
  return TextStyle(
    fontFamily: 'Menlo',
    fontSize: fontSize,
    color: color,
    fontWeight: fontWeight,
  );
}

class ApiException implements Exception {
  ApiException(this.message, {this.statusCode});

  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

class SessionStore extends ChangeNotifier {
  SessionStore({this.accessToken, this.refreshToken});

  String? accessToken;
  String? refreshToken;

  bool get isAuthorized =>
      accessToken != null && accessToken!.isNotEmpty && refreshToken != null;

  static const _accessKey = 'access_token';
  static const _refreshKey = 'refresh_token';

  static Future<SessionStore> load() async {
    final prefs = await SharedPreferences.getInstance();
    return SessionStore(
      accessToken: prefs.getString(_accessKey),
      refreshToken: prefs.getString(_refreshKey),
    );
  }

  Future<void> setTokens({
    required String access,
    required String refresh,
  }) async {
    accessToken = access;
    refreshToken = refresh;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_accessKey, access);
    await prefs.setString(_refreshKey, refresh);
    notifyListeners();
  }

  Future<void> clear() async {
    accessToken = null;
    refreshToken = null;
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_accessKey);
    await prefs.remove(_refreshKey);
    notifyListeners();
  }
}

class TokenPair {
  TokenPair({required this.accessToken, required this.refreshToken});

  final String accessToken;
  final String refreshToken;
}

class UserModel {
  UserModel({
    required this.id,
    required this.username,
    required this.phone,
    required this.createdAt,
  });

  final String id;
  final String username;
  final String phone;
  final String createdAt;

  factory UserModel.fromJson(Map<String, dynamic> json) => UserModel(
    id: json['id']?.toString() ?? '',
    username: json['username']?.toString() ?? '',
    phone: json['phone']?.toString() ?? '',
    createdAt: json['created_at']?.toString() ?? '',
  );
}

class MessageModel {
  MessageModel({
    required this.id,
    required this.senderId,
    required this.receiverId,
    required this.text,
    required this.isDelivered,
    required this.createdAt,
  });

  final String id;
  final String senderId;
  final String receiverId;
  final String text;
  final bool isDelivered;
  final String createdAt;

  factory MessageModel.fromJson(Map<String, dynamic> json) => MessageModel(
    id: json['id']?.toString() ?? '',
    senderId: json['sender_id']?.toString() ?? '',
    receiverId: json['receiver_id']?.toString() ?? '',
    text: json['text']?.toString() ?? '',
    isDelivered: (json['is_delivered'] as bool?) ?? false,
    createdAt: json['created_at']?.toString() ?? '',
  );
}

class RecentChat {
  RecentChat({
    required this.id,
    required this.username,
    required this.phone,
    required this.lastMessage,
    required this.lastMessageAt,
  });

  final String id;
  final String username;
  final String phone;
  final String lastMessage;
  final String lastMessageAt;

  UserModel toUserModel() => UserModel(
    id: id,
    username: username,
    phone: phone,
    createdAt: '',
  );

  Map<String, dynamic> toJson() => {
    'id': id,
    'username': username,
    'phone': phone,
    'last_message': lastMessage,
    'last_message_at': lastMessageAt,
  };

  factory RecentChat.fromJson(Map<String, dynamic> json) => RecentChat(
    id: json['id']?.toString() ?? '',
    username: json['username']?.toString() ?? '',
    phone: json['phone']?.toString() ?? '',
    lastMessage: json['last_message']?.toString() ?? '',
    lastMessageAt: json['last_message_at']?.toString() ?? '',
  );
}

class RecentChatsStore {
  static const _key = 'recent_chats_v1';
  static const _maxItems = 30;

  static Future<List<RecentChat>> load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_key);
    if (raw == null || raw.isEmpty) return const [];
    try {
      final list = jsonDecode(raw) as List<dynamic>;
      return list
          .whereType<Map<String, dynamic>>()
          .map(RecentChat.fromJson)
          .where((c) => c.id.isNotEmpty)
          .toList();
    } catch (_) {
      return const [];
    }
  }

  static Future<void> save(List<RecentChat> chats) async {
    final prefs = await SharedPreferences.getInstance();
    final limited = chats.take(_maxItems).map((c) => c.toJson()).toList();
    await prefs.setString(_key, jsonEncode(limited));
  }
}

class ApiClient {
  ApiClient({required this.baseUrl, required this.session});

  final String baseUrl;
  final SessionStore session;

  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? query,
    bool auth = false,
    bool allowRetry = true,
  }) async {
    var uri = Uri.parse('$baseUrl$path');
    if (query != null && query.isNotEmpty) {
      uri = uri.replace(queryParameters: query);
    }

    final headers = <String, String>{'Content-Type': 'application/json'};
    if (auth && session.accessToken != null) {
      headers['Authorization'] = 'Bearer ${session.accessToken}';
    }

    late http.Response response;
    final encoded = body == null ? null : jsonEncode(body);

    switch (method) {
      case 'GET':
        response = await http.get(uri, headers: headers);
        break;
      case 'POST':
        response = await http.post(uri, headers: headers, body: encoded);
        break;
      default:
        throw ApiException('Unsupported method: $method');
    }

    if (response.statusCode == 401 &&
        auth &&
        allowRetry &&
        session.refreshToken != null) {
      final ok = await _refreshTokens();
      if (ok) {
        return _request(
          method,
          path,
          body: body,
          query: query,
          auth: auth,
          allowRetry: false,
        );
      }
    }

    final decoded = response.body.isEmpty
        ? <String, dynamic>{}
        : (jsonDecode(response.body) as Map<String, dynamic>);

    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw ApiException(
        decoded['message']?.toString() ?? 'HTTP ${response.statusCode}',
        statusCode: response.statusCode,
      );
    }

    return decoded;
  }

  Future<bool> _refreshTokens() async {
    final refresh = session.refreshToken;
    if (refresh == null || refresh.isEmpty) {
      await session.clear();
      return false;
    }

    try {
      final decoded = await _request(
        'POST',
        '/v1/auth/refresh',
        body: {'refresh_token': refresh},
        auth: false,
        allowRetry: false,
      );
      final tokens = decoded['tokens'] as Map<String, dynamic>?;
      if (tokens == null) {
        await session.clear();
        return false;
      }
      await session.setTokens(
        access: tokens['access_token']?.toString() ?? '',
        refresh: tokens['refresh_token']?.toString() ?? '',
      );
      return true;
    } catch (_) {
      await session.clear();
      return false;
    }
  }

  Future<void> register({
    required String username,
    required String phone,
    required String password,
  }) async {
    await _request(
      'POST',
      '/v1/auth/register',
      body: {'username': username, 'phone': phone, 'password': password},
    );
  }

  Future<TokenPair> login({
    required String username,
    required String password,
  }) async {
    final decoded = await _request(
      'POST',
      '/v1/auth/login',
      body: {'username': username, 'password': password},
    );

    final tokens = decoded['tokens'] as Map<String, dynamic>?;
    if (tokens == null) {
      throw ApiException('Не удалось получить токены');
    }

    return TokenPair(
      accessToken: tokens['access_token']?.toString() ?? '',
      refreshToken: tokens['refresh_token']?.toString() ?? '',
    );
  }

  Future<void> logout() async {
    final refresh = session.refreshToken;
    if (refresh != null && refresh.isNotEmpty) {
      await _request(
        'POST',
        '/v1/auth/logout',
        body: {'refresh_token': refresh},
        auth: true,
        allowRetry: false,
      );
    }
    await session.clear();
  }

  Future<UserModel> getMe() async {
    final decoded = await _request('GET', '/v1/users/me', auth: true);
    final user = decoded['user'] as Map<String, dynamic>?;
    if (user == null) {
      throw ApiException('Профиль не получен');
    }
    return UserModel.fromJson(user);
  }

  Future<List<UserModel>> searchUsers(String username) async {
    final decoded = await _request(
      'GET',
      '/v1/users/search',
      query: {'username': username},
      auth: true,
    );

    final users = decoded['users'] as List<dynamic>? ?? const [];
    return users
        .whereType<Map<String, dynamic>>()
        .map(UserModel.fromJson)
        .toList();
  }

  Future<List<MessageModel>> getHistory(String peerUserId) async {
    final decoded = await _request(
      'GET',
      '/v1/messages/$peerUserId',
      auth: true,
    );
    final messages = decoded['messages'] as List<dynamic>? ?? const [];
    return messages
        .whereType<Map<String, dynamic>>()
        .map(MessageModel.fromJson)
        .toList();
  }

  Future<void> sendMessage({
    required String toUsername,
    required String text,
  }) async {
    await _request(
      'POST',
      '/v1/messages/send',
      auth: true,
      body: {'to_username': toUsername, 'text': text},
    );
  }

  Future<String> issueWsTicket() async {
    final decoded = await _request('POST', '/v1/auth/ws-ticket', auth: true);
    final ticket = decoded['ticket']?.toString() ?? '';
    if (ticket.isEmpty) {
      throw ApiException('Не удалось получить WS ticket');
    }
    return ticket;
  }
}

class AuthScreen extends StatefulWidget {
  const AuthScreen({super.key, required this.api, required this.session});

  final ApiClient api;
  final SessionStore session;

  @override
  State<AuthScreen> createState() => _AuthScreenState();
}

class _AuthScreenState extends State<AuthScreen> {
  bool _isLogin = true;
  bool _loading = false;
  String? _error;

  final _username = TextEditingController();
  final _phone = TextEditingController();
  final _password = TextEditingController();

  Future<void> _submit() async {
    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      final username = _username.text.trim();
      final password = _password.text.trim();
      if (username.isEmpty || password.isEmpty) {
        throw ApiException('Заполни username и password');
      }

      if (_isLogin) {
        final pair = await widget.api.login(
          username: username,
          password: password,
        );
        await widget.session.setTokens(
          access: pair.accessToken,
          refresh: pair.refreshToken,
        );
      } else {
        final phone = _phone.text.trim();
        if (phone.isEmpty) {
          throw ApiException('Заполни телефон');
        }
        await widget.api.register(
          username: username,
          phone: phone,
          password: password,
        );
        final pair = await widget.api.login(
          username: username,
          password: password,
        );
        await widget.session.setTokens(
          access: pair.accessToken,
          refresh: pair.refreshToken,
        );
      }
    } catch (e) {
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const SizedBox(height: 28),
                  Center(
                    child: Container(
                      width: 56,
                      height: 56,
                      decoration: BoxDecoration(
                        color: AppColors.accent,
                        borderRadius: BorderRadius.circular(16),
                      ),
                      alignment: Alignment.center,
                      child: const Text(
                        'V',
                        style: TextStyle(
                          color: Colors.white,
                          fontSize: 24,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 12),
                  const Center(
                    child: Text(
                      'Vual',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    _isLogin ? 'Вход в аккаунт' : 'Создать аккаунт',
                    textAlign: TextAlign.center,
                    style: monoStyle(),
                  ),
                  const SizedBox(height: 20),
                  Expanded(
                    child: SingleChildScrollView(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          _Input(
                            label: 'Имя пользователя',
                            hint: 'alex_vual',
                            controller: _username,
                          ),
                          const SizedBox(height: 12),
                          if (!_isLogin) ...[
                            _Input(
                              label: 'Телефон',
                              hint: '+7 999 000 00 00',
                              controller: _phone,
                            ),
                            const SizedBox(height: 12),
                          ],
                          _Input(
                            label: 'Пароль',
                            hint: '••••••••',
                            isPassword: true,
                            controller: _password,
                          ),
                          const SizedBox(height: 12),
                          if (_error != null)
                            Container(
                              padding: const EdgeInsets.all(10),
                              decoration: BoxDecoration(
                                color: AppColors.red.withValues(alpha: 0.12),
                                borderRadius: BorderRadius.circular(10),
                                border: Border.all(
                                  color: AppColors.red.withValues(alpha: 0.4),
                                ),
                              ),
                              child: Text(
                                _error!,
                                style: const TextStyle(
                                  color: AppColors.red,
                                  fontSize: 12,
                                ),
                              ),
                            ),
                          const SizedBox(height: 12),
                          FilledButton(
                            style: FilledButton.styleFrom(
                              backgroundColor: AppColors.accent,
                              padding: const EdgeInsets.symmetric(vertical: 12),
                              shape: RoundedRectangleBorder(
                                borderRadius: BorderRadius.circular(10),
                              ),
                            ),
                            onPressed: _loading ? null : _submit,
                            child: Text(
                              _loading
                                  ? 'Загрузка...'
                                  : (_isLogin ? 'Войти' : 'Зарегистрироваться'),
                            ),
                          ),
                          const SizedBox(height: 8),
                          TextButton(
                            onPressed: _loading
                                ? null
                                : () {
                                    setState(() {
                                      _isLogin = !_isLogin;
                                      _error = null;
                                    });
                                  },
                            child: Text(
                              _isLogin
                                  ? 'Нет аккаунта? Регистрация'
                                  : 'Уже есть аккаунт? Войти',
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class ChatListScreen extends StatefulWidget {
  const ChatListScreen({super.key, required this.api, required this.session});

  final ApiClient api;
  final SessionStore session;

  @override
  State<ChatListScreen> createState() => _ChatListScreenState();
}

class _ChatListScreenState extends State<ChatListScreen> {
  final _search = TextEditingController();

  UserModel? _me;
  List<UserModel> _users = [];
  List<RecentChat> _recentChats = [];
  bool _loading = true;
  bool _searching = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadMe();
  }

  Future<void> _loadMe() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final me = await widget.api.getMe();
      final recents = await RecentChatsStore.load();
      if (!mounted) return;
      setState(() {
        _me = me;
        _recentChats = recents;
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _openChat(UserModel user) async {
    final me = _me;
    if (me == null) {
      setState(() {
        _error = 'Профиль еще загружается, попробуй через секунду';
      });
      return;
    }

    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => ChatScreen(
          api: widget.api,
          me: me,
          peer: user,
          onHistoryUpdated: _upsertRecentFromHistory,
        ),
      ),
    );

    if (mounted) {
      setState(() {});
    }
  }

  Future<void> _upsertRecentFromHistory(
    UserModel peer,
    List<MessageModel> history,
  ) async {
    final latest = history.isNotEmpty ? history.last : null;
    final preview = RecentChat(
      id: peer.id,
      username: peer.username,
      phone: peer.phone,
      lastMessage: latest?.text ?? '',
      lastMessageAt: latest?.createdAt ?? '',
    );

    final updated = [preview, ..._recentChats.where((c) => c.id != peer.id)];
    await RecentChatsStore.save(updated);
    if (!mounted) return;
    setState(() {
      _recentChats = updated;
    });
  }

  Future<void> _doSearch() async {
    final value = _search.text.trim();
    if (value.isEmpty) return;

    setState(() {
      _searching = true;
      _error = null;
    });
    try {
      final users = await widget.api.searchUsers(value);
      if (!mounted) return;
      setState(() {
        _users = users.where((u) => u.id != _me?.id).toList();
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _searching = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        backgroundColor: AppColors.bgElevated,
        title: Text(_me == null ? 'Vual' : 'Vual · ${_me!.username}'),
        actions: [
          IconButton(
            onPressed: () async {
              await Navigator.of(context).push(
                MaterialPageRoute(
                  builder: (_) => ProfileScreen(api: widget.api),
                ),
              );
              _loadMe();
            },
            icon: const Icon(Icons.person_outline),
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : Column(
              children: [
                Container(
                  margin: const EdgeInsets.fromLTRB(12, 12, 12, 8),
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  decoration: BoxDecoration(
                    color: AppColors.bgInput,
                    borderRadius: BorderRadius.circular(20),
                  ),
                  child: Row(
                    children: [
                      const Icon(
                        Icons.search,
                        color: AppColors.textMuted,
                        size: 18,
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: TextField(
                          controller: _search,
                          style: const TextStyle(color: AppColors.textPrimary),
                          decoration: const InputDecoration(
                            hintText: 'Поиск по username',
                            hintStyle: TextStyle(color: AppColors.textMuted),
                            border: InputBorder.none,
                          ),
                          onSubmitted: (_) => _doSearch(),
                        ),
                      ),
                      if (_searching)
                        const SizedBox(
                          width: 14,
                          height: 14,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      else
                        IconButton(
                          onPressed: _doSearch,
                          icon: const Icon(Icons.arrow_forward, size: 18),
                        ),
                    ],
                  ),
                ),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 6,
                    ),
                    child: Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: AppColors.red.withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        _error!.replaceFirst('Exception: ', ''),
                        style: const TextStyle(color: AppColors.red),
                      ),
                    ),
                  ),
                Expanded(
                  child: _users.isNotEmpty
                      ? ListView.builder(
                          itemCount: _users.length,
                          itemBuilder: (context, index) {
                            final user = _users[index];
                            return ListTile(
                              onTap: () => _openChat(user),
                              leading: CircleAvatar(
                                backgroundColor: AppColors.accent,
                                child: Text(
                                  user.username.isEmpty
                                      ? '?'
                                      : user.username[0].toUpperCase(),
                                ),
                              ),
                              title: Text(user.username),
                              subtitle: Text(
                                user.phone,
                                style: const TextStyle(
                                  color: AppColors.textSecondary,
                                ),
                              ),
                            );
                          },
                        )
                      : _recentChats.isEmpty
                      ? const Center(
                          child: Text(
                            'Начни с поиска пользователя',
                            style: TextStyle(color: AppColors.textSecondary),
                          ),
                        )
                      : ListView.separated(
                          itemCount: _recentChats.length + 1,
                          separatorBuilder: (_, _) => const Divider(
                            height: 1,
                            color: Color(0x22FFFFFF),
                          ),
                          itemBuilder: (context, index) {
                            if (index == 0) {
                              return const Padding(
                                padding: EdgeInsets.fromLTRB(16, 8, 16, 8),
                                child: Text(
                                  'Недавние чаты',
                                  style: TextStyle(
                                    color: AppColors.textSecondary,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              );
                            }

                            final chat = _recentChats[index - 1];
                            final user = chat.toUserModel();
                            return ListTile(
                              onTap: () => _openChat(user),
                              leading: CircleAvatar(
                                backgroundColor: AppColors.accent,
                                child: Text(
                                  chat.username.isEmpty
                                      ? '?'
                                      : chat.username[0].toUpperCase(),
                                ),
                              ),
                              title: Text(
                                chat.username.isEmpty ? 'Без имени' : chat.username,
                              ),
                              subtitle: Text(
                                chat.lastMessage.isEmpty
                                    ? chat.phone
                                    : chat.lastMessage,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  color: AppColors.textSecondary,
                                ),
                              ),
                              trailing: Text(
                                _formatTime(chat.lastMessageAt),
                                style: monoStyle(
                                  fontSize: 10,
                                  color: AppColors.textMuted,
                                ),
                              ),
                            );
                          },
                        ),
                ),
              ],
            ),
    );
  }

  String _formatTime(String iso) {
    final dt = DateTime.tryParse(iso)?.toLocal();
    if (dt == null) return '';
    final h = dt.hour.toString().padLeft(2, '0');
    final m = dt.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }
}

class ChatScreen extends StatefulWidget {
  const ChatScreen({
    super.key,
    required this.api,
    required this.me,
    required this.peer,
    this.onHistoryUpdated,
  });

  final ApiClient api;
  final UserModel me;
  final UserModel peer;
  final Future<void> Function(UserModel peer, List<MessageModel> history)?
  onHistoryUpdated;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _controller = TextEditingController();

  bool _loading = true;
  bool _sending = false;
  List<MessageModel> _messages = [];
  WebSocket? _ws;
  Timer? _pollTimer;
  Timer? _reconnectTimer;
  bool _disposed = false;
  bool _wsConnecting = false;
  int _reconnectAttempt = 0;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadHistory();
    _connectWs();
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) {
      _refreshHistorySilently();
    });
  }

  @override
  void dispose() {
    _disposed = true;
    _controller.dispose();
    _pollTimer?.cancel();
    _reconnectTimer?.cancel();
    _ws?.close();
    super.dispose();
  }

  Future<void> _connectWs() async {
    if (_disposed || _wsConnecting || _ws != null) return;
    _wsConnecting = true;
    try {
      final ticket = await widget.api.issueWsTicket();
      final base = Uri.parse(widget.api.baseUrl);
      final wsUri = Uri(
        scheme: base.scheme == 'https' ? 'wss' : 'ws',
        host: base.host,
        port: base.hasPort ? base.port : null,
        path: '/ws',
        queryParameters: {'ticket': ticket},
      );

      final ws = await WebSocket.connect(wsUri.toString());
      if (_disposed) {
        await ws.close();
        return;
      }
      _ws = ws;
      _reconnectAttempt = 0;
      _reconnectTimer?.cancel();
      ws.listen(
        _handleWsEvent,
        onError: (_) {
          _ws = null;
          _scheduleReconnect();
        },
        onDone: () {
          _ws = null;
          _scheduleReconnect();
        },
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    } finally {
      _wsConnecting = false;
    }
  }

  void _scheduleReconnect() {
    if (_disposed || _ws != null) return;
    if (_reconnectTimer?.isActive ?? false) return;
    final delaySec = (1 << _reconnectAttempt).clamp(1, 30);
    _reconnectAttempt = (_reconnectAttempt + 1).clamp(0, 10);
    _reconnectTimer = Timer(Duration(seconds: delaySec), () {
      _connectWs();
    });
  }

  void _handleWsEvent(dynamic event) {
    if (event is! String) return;
    Map<String, dynamic> data;
    try {
      data = jsonDecode(event) as Map<String, dynamic>;
    } catch (_) {
      return;
    }

    final type = data['type']?.toString() ?? '';
    if (type != 'message') return;

    final fromUser = data['from_user']?.toString() ?? '';
    if (fromUser != widget.peer.id) return;

    final msg = MessageModel(
      id: data['message_id']?.toString() ?? '',
      senderId: fromUser,
      receiverId: widget.me.id,
      text: data['text']?.toString() ?? '',
      isDelivered: true,
      createdAt: data['created_at']?.toString() ?? DateTime.now().toIso8601String(),
    );

    if (!mounted) return;
    setState(() {
      _messages = [..._messages, msg];
    });
  }

  Future<void> _refreshHistorySilently() async {
    if (!mounted || _loading) return;
    try {
      final history = await widget.api.getHistory(widget.peer.id);
      final asc = history.reversed.toList();
      await widget.onHistoryUpdated?.call(widget.peer, history);
      if (!mounted) return;
      if (_sameMessages(_messages, asc)) return;
      setState(() {
        _messages = asc;
      });
    } catch (_) {
      // Тихий фоновый рефреш не должен ломать UI.
    }
  }

  bool _sameMessages(List<MessageModel> a, List<MessageModel> b) {
    if (identical(a, b)) return true;
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i].id != b[i].id) return false;
      if (a[i].text != b[i].text) return false;
      if (a[i].isDelivered != b[i].isDelivered) return false;
    }
    return true;
  }

  Future<void> _loadHistory() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final history = await widget.api.getHistory(widget.peer.id);
      await widget.onHistoryUpdated?.call(widget.peer, history);
      if (!mounted) return;
      setState(() {
        _messages = history.reversed.toList();
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _send() async {
    final text = _controller.text.trim();
    if (text.isEmpty || _sending) return;

    setState(() {
      _sending = true;
      _error = null;
    });
    try {
      await widget.api.sendMessage(
        toUsername: widget.peer.username,
        text: text,
      );
      _controller.clear();
      await _loadHistory();
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _sending = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        backgroundColor: AppColors.bgElevated,
        title: Text(widget.peer.username),
      ),
      body: Column(
        children: [
          if (_error != null)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: AppColors.red.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Text(
                  _error!.replaceFirst('Exception: ', ''),
                  style: const TextStyle(color: AppColors.red),
                ),
              ),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : ListView.builder(
                    padding: const EdgeInsets.symmetric(
                      vertical: 10,
                      horizontal: 12,
                    ),
                    itemCount: _messages.length,
                    itemBuilder: (context, index) {
                      final m = _messages[index];
                      final outgoing = m.senderId == widget.me.id;
                      return Align(
                        alignment: outgoing
                            ? Alignment.centerRight
                            : Alignment.centerLeft,
                        child: Container(
                          margin: const EdgeInsets.symmetric(vertical: 4),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 8,
                          ),
                          constraints: const BoxConstraints(maxWidth: 290),
                          decoration: BoxDecoration(
                            color: outgoing
                                ? AppColors.accent
                                : AppColors.bgElevated,
                            borderRadius: BorderRadius.circular(16),
                          ),
                          child: Column(
                            crossAxisAlignment: outgoing
                                ? CrossAxisAlignment.end
                                : CrossAxisAlignment.start,
                            children: [
                              Text(m.text),
                              const SizedBox(height: 4),
                              Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Text(
                                    _formatTime(m.createdAt),
                                    style: const TextStyle(
                                      fontSize: 10,
                                      color: AppColors.textMuted,
                                    ),
                                  ),
                                  if (outgoing) ...[
                                    const SizedBox(width: 4),
                                    Icon(
                                      m.isDelivered
                                          ? Icons.done_all
                                          : Icons.done,
                                      size: 12,
                                      color: Colors.white,
                                    ),
                                  ],
                                ],
                              ),
                            ],
                          ),
                        ),
                      );
                    },
                  ),
          ),
          Container(
            color: AppColors.bgElevated,
            padding: const EdgeInsets.fromLTRB(10, 10, 10, 14),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _controller,
                    decoration: InputDecoration(
                      hintText: 'Сообщение...',
                      hintStyle: const TextStyle(
                        color: AppColors.textSecondary,
                      ),
                      filled: true,
                      fillColor: AppColors.bgInput,
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(22),
                        borderSide: BorderSide.none,
                      ),
                      contentPadding: const EdgeInsets.symmetric(
                        horizontal: 14,
                        vertical: 10,
                      ),
                    ),
                    onSubmitted: (_) => _send(),
                  ),
                ),
                const SizedBox(width: 8),
                CircleAvatar(
                  radius: 16,
                  backgroundColor: AppColors.accent,
                  child: IconButton(
                    onPressed: _sending ? null : _send,
                    icon: Icon(
                      Icons.send,
                      size: 16,
                      color: _sending ? Colors.white54 : Colors.white,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  String _formatTime(String iso) {
    final dt = DateTime.tryParse(iso)?.toLocal();
    if (dt == null) return '';
    final h = dt.hour.toString().padLeft(2, '0');
    final m = dt.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }
}

class ProfileScreen extends StatefulWidget {
  const ProfileScreen({super.key, required this.api});

  final ApiClient api;

  @override
  State<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends State<ProfileScreen> {
  UserModel? _user;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final me = await widget.api.getMe();
      if (!mounted) return;
      setState(() {
        _user = me;
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        backgroundColor: AppColors.bgElevated,
        title: const Text('Профиль'),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : (_error != null)
          ? Center(
              child: Text(
                _error!.replaceFirst('Exception: ', ''),
                style: const TextStyle(color: AppColors.red),
              ),
            )
          : ListView(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(vertical: 24),
                  decoration: const BoxDecoration(
                    gradient: LinearGradient(
                      colors: [AppColors.bgElevated, AppColors.bgSurface],
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                    ),
                  ),
                  child: Column(
                    children: [
                      CircleAvatar(
                        radius: 32,
                        backgroundColor: AppColors.accent,
                        child: Text(
                          _user!.username.isEmpty
                              ? '?'
                              : _user!.username[0].toUpperCase(),
                          style: const TextStyle(fontSize: 24),
                        ),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        _user!.username,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        _user!.phone,
                        style: monoStyle(
                          color: AppColors.textSecondary,
                          fontSize: 11,
                        ),
                      ),
                    ],
                  ),
                ),
                _ProfileTile(
                  icon: Icons.alternate_email,
                  title: 'Имя пользователя',
                  value: _user!.username,
                ),
                _ProfileTile(
                  icon: Icons.call_outlined,
                  title: 'Телефон',
                  value: _user!.phone,
                ),
                _ProfileTile(
                  icon: Icons.calendar_month_outlined,
                  title: 'Регистрация',
                  value: _user!.createdAt,
                ),
                const SizedBox(height: 10),
                ListTile(
                  leading: const Icon(Icons.logout, color: AppColors.red),
                  title: const Text(
                    'Выйти',
                    style: TextStyle(color: AppColors.red),
                  ),
                  onTap: () async {
                    try {
                      await widget.api.logout();
                      if (!context.mounted) return;
                      Navigator.of(context).pop();
                    } catch (e) {
                      ScaffoldMessenger.of(
                        context,
                      ).showSnackBar(SnackBar(content: Text(e.toString())));
                    }
                  },
                ),
              ],
            ),
    );
  }
}

class _ProfileTile extends StatelessWidget {
  const _ProfileTile({
    required this.icon,
    required this.title,
    required this.value,
  });

  final IconData icon;
  final String title;
  final String value;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, color: AppColors.textMuted),
      title: Text(title),
      trailing: SizedBox(
        width: 180,
        child: Text(
          value,
          textAlign: TextAlign.right,
          overflow: TextOverflow.ellipsis,
          style: monoStyle(fontSize: 11, color: AppColors.textSecondary),
        ),
      ),
    );
  }
}

class _Input extends StatelessWidget {
  const _Input({
    required this.label,
    required this.hint,
    required this.controller,
    this.isPassword = false,
  });

  final String label;
  final String hint;
  final TextEditingController controller;
  final bool isPassword;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label.toUpperCase(),
          style: const TextStyle(
            fontSize: 10,
            color: AppColors.textSecondary,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 6),
        TextField(
          controller: controller,
          obscureText: isPassword,
          decoration: InputDecoration(
            hintText: hint,
            hintStyle: const TextStyle(color: AppColors.textSecondary),
            filled: true,
            fillColor: AppColors.bgInput,
            enabledBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(10),
              borderSide: BorderSide(
                color: Colors.white.withValues(alpha: 0.12),
              ),
            ),
            focusedBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(10),
              borderSide: const BorderSide(color: AppColors.accent),
            ),
          ),
        ),
      ],
    );
  }
}
