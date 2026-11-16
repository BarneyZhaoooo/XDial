package com.xd.xdial.android;

import android.app.*;
import android.content.*;
import android.net.*;
import android.os.*;
import java.net.NetworkInterface;
import java.util.concurrent.*;
import org.json.*;
import libbox.*;

public final class TunnelService extends VpnService {
 public static volatile String state = "未连接";
 public static volatile boolean active;
 public static volatile String probeResult="";
 private final ExecutorService worker = Executors.newSingleThreadExecutor();
 private Libbox_ core;
 private ParcelFileDescriptor tunnel;
 private ConnectivityManager connectivity;
 private volatile Network underlay;
 private boolean watching;
 private final java.util.concurrent.atomic.AtomicLong protectedSockets = new java.util.concurrent.atomic.AtomicLong();
 private final java.util.concurrent.atomic.AtomicLong protectionFailures = new java.util.concurrent.atomic.AtomicLong();
 private String stage="配置读取";
 private final ConnectivityManager.NetworkCallback networks = new ConnectivityManager.NetworkCallback() {
  @Override public void onLost(Network network) {
   if (network.equals(underlay)) worker.execute(() -> { state="底层网络已断开，请重新连接"; shutdown(); });
  }
 };
 @Override public void onCreate() { super.onCreate(); connectivity=getSystemService(ConnectivityManager.class); }
 @Override public int onStartCommand(Intent intent,int flags,int id) {
  if(intent!=null && "probe".equals(intent.getAction())) {
   worker.execute(() -> { if(core!=null) {probeResult=ConnectionProbe.run(connectivity); android.util.Log.i("XDialProbe",probeResult);} });
   return START_NOT_STICKY;
  }
  if(intent!=null && "diagnose".equals(intent.getAction())) {
   worker.execute(() -> {
    try {
     JSONObject facts = core == null ? new JSONObject() : new JSONObject(core.diagnostics());
     JSONObject safe = new JSONObject();
     for(String key : new String[]{"running", "gvisor_compiled", "tun_fd_ready"}) safe.put(key, facts.opt(key));
     if(core!=null) safe.put("routed_traffic", new JSONObject(core.trafficTotals()));
     safe.put("protected_sockets", protectedSockets.get()); safe.put("protection_failures", protectionFailures.get());
     android.util.Log.i("XDialDiagnostics", safe.toString());
    } catch(Exception ignored) { }
   });
   return START_NOT_STICKY;
  }
  if(intent!=null && "stop".equals(intent.getAction())) { worker.execute(() -> { state="未连接"; shutdown(); }); return START_NOT_STICKY; }
  NotificationManager nm=getSystemService(NotificationManager.class);
  nm.createNotificationChannel(new NotificationChannel("connection","连接状态",NotificationManager.IMPORTANCE_LOW));
  PendingIntent open=PendingIntent.getActivity(this,0,new Intent(this,MainActivity.class),PendingIntent.FLAG_IMMUTABLE);
  startForeground(1,new Notification.Builder(this,"connection").setSmallIcon(android.R.drawable.stat_sys_upload_done)
    .setContentTitle("XDial").setContentText("网络连接服务正在运行").setContentIntent(open).setOngoing(true).build());
  worker.execute(this::connect);
  return START_NOT_STICKY;
 }
 private void connect() {
  if(core!=null) return;
  active=true; state="准备连接";
  try {
   stage="配置读取";
   String profile=new ProfileStore(this).load();
   // Until protocol-owned socket coverage is proven, do not expose enterprise
   // protocols in this development checkpoint.
   JSONObject doc=new JSONObject(profile);
   JSONArray lines=doc.optJSONArray("lines");
   if(lines!=null) for(int i=0;i<lines.length();i++) {
    String type=lines.getJSONObject(i).optString("type");
    if(type.equals("vpn")||type.equals("tailscale")) throw new IllegalStateException("Unsupported checkpoint protocol");
   }
   stage="系统网络";
   underlay=connectivity.getActiveNetwork();
   if(underlay==null) throw new IllegalStateException("No Underlay");
   NetworkCapabilities capabilities=connectivity.getNetworkCapabilities(underlay);
   // Android permits only one VpnService in this user/profile. Never replace
   // an existing VPN as if it were an independently composable Underlay.
   if(capabilities==null || capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) throw new IllegalStateException("Existing VPN");
   LinkProperties links=connectivity.getLinkProperties(underlay);
   NetworkInterface nic=NetworkInterface.getByName(links.getInterfaceName());
   final Network captured=underlay;
   stage="配置编译";
   JSONArray systemDNS = new JSONArray();
   for(java.net.InetAddress server : links.getDnsServers()) systemDNS.put(server.getHostAddress());
   String config=Libbox.generateAndroidConfig(profile,systemDNS.toString(),getNoBackupFilesDir().getAbsolutePath());
   JSONObject generated=new JSONObject(config);
   JSONArray inbounds=generated.getJSONArray("inbounds"); JSONObject tun=null;
   for(int i=0;i<inbounds.length();i++) if(inbounds.getJSONObject(i).getString("type").equals("tun")) tun=inbounds.getJSONObject(i);
   if(tun==null) throw new IllegalStateException("No TUN");
   Builder builder=new Builder().setSession("XDial").setMtu(tun.getInt("mtu"));
   JSONArray addresses=tun.getJSONArray("address");
   for(int i=0;i<addresses.length();i++) { String[] cidr=addresses.getString(i).split("/"); builder.addAddress(cidr[0],Integer.parseInt(cidr[1])); }
   builder.addRoute("0.0.0.0",0).addRoute("::",0).addDnsServer("198.18.0.2");
   builder.setUnderlyingNetworks(new Network[]{captured});
   stage="核心初始化";
   core=Libbox.new_(new Callback() {
    public void onStatusChanged(String json) { }
    public void onError(long code,String message) { worker.execute(() -> { state="连接错误（"+code+"），请重新连接"; shutdown(); }); }
   });
   core.setSocketProtector(fd -> {
    try {
     if(!protect((int)fd)) throw new IllegalStateException("Socket protection denied");
     try(ParcelFileDescriptor duplicate=ParcelFileDescriptor.fromFd((int)fd)) { captured.bindSocket(duplicate.getFileDescriptor()); }
     protectedSockets.incrementAndGet();
    } catch(Exception e) { protectionFailures.incrementAndGet(); throw e; }
   });
   JSONArray snapshots = new JSONArray();
   JSONObject facts = new JSONObject();
   facts.put("mtu", nic.getMTU());
   int interfaceFlags = (nic.isUp()?1:0) | (nic.isLoopback()?4:0) | (nic.isPointToPoint()?8:0) | (nic.supportsMulticast()?16:0);
   facts.put("flags", interfaceFlags);
   JSONArray prefixes = new JSONArray();
   for(LinkAddress address : links.getLinkAddresses()) prefixes.put(address.getAddress().getHostAddress().split("%")[0]+"/"+address.getPrefixLength());
   facts.put("addresses", prefixes);
   JSONObject snapshot = new JSONObject();
   snapshot.put("name", nic.getName()); snapshot.put("index", nic.getIndex());
   snapshot.put("type", capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)?"wifi":capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)?"cellular":"other");
   snapshot.put("facts", facts); snapshots.put(snapshot);
   core.setNetworkInterfaces(snapshots.toString());
   core.setDefaultInterface(nic.getName(),nic.getIndex());
   stage="建立 TUN";
   tunnel=builder.establish();
   if(tunnel==null) throw new IllegalStateException("VPN permission revoked");
   core.setTunFD(tunnel.getFd());
   connectivity.registerNetworkCallback(new NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(),networks);
   watching=true;
   stage="启动数据面";
   core.startStandalone(config);
   state="隧道已启动（流量待验证）";
  } catch(Exception error) { state=stage+"失败（"+error.getClass().getSimpleName()+"）"; shutdown(); }
 }
 private void shutdown() {
  if(watching) { connectivity.unregisterNetworkCallback(networks); watching=false; }
  if(core!=null) { try { core.stop(); } catch(Exception ignored) {} core=null; }
  if(tunnel!=null) { try { tunnel.close(); } catch(Exception ignored) {} tunnel=null; }
  underlay=null; active=false; stopForeground(STOP_FOREGROUND_REMOVE); stopSelf();
 }
 @Override public void onRevoke() { worker.execute(() -> { state="VPN 授权已撤销"; shutdown(); }); }
 @Override public void onDestroy() { worker.execute(this::shutdown); worker.shutdown(); super.onDestroy(); }
}
