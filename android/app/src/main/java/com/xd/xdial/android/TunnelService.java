package com.xd.xdial.android;

import android.app.*;
import android.content.*;
import android.net.*;
import android.os.*;
import java.net.NetworkInterface;
import java.util.concurrent.*;
import org.json.*;
import libbox.*;

/** Android owns OS resources; the shared controller owns intent/epoch/retries. */
public final class TunnelService extends VpnService {
 public static volatile String state="未连接", probeResult="";
 public static volatile boolean active, settingUp;
 public static volatile String tailscaleStatus="", setupMessage="";
 private String setupLine="";
 private ScheduledFuture<?> statusRefresh;
 private final ScheduledExecutorService worker=Executors.newSingleThreadScheduledExecutor();
 private final ExecutorService probes=Executors.newSingleThreadExecutor();
 private final ConnectionController lifecycle=Libbox.newConnectionController();
 private Libbox_ core;
 private ParcelFileDescriptor tunnel;
 private ConnectivityManager connectivity;
 private Network underlay;
 private LinkProperties links;
 private NetworkCapabilities capabilities;
 private boolean watching,blocked;
 private volatile boolean destroyed;
 private long coreEpoch;
 private ScheduledFuture<?> retry;
 private String stage="配置读取";
 private final java.util.concurrent.atomic.AtomicLong protectedSockets=new java.util.concurrent.atomic.AtomicLong();
 private final java.util.concurrent.atomic.AtomicLong protectionFailures=new java.util.concurrent.atomic.AtomicLong();
 private void enqueue(Runnable action) {
  if(destroyed)return;
  try {worker.execute(()->{if(!destroyed)action.run();});}catch(RejectedExecutionException ignored){}
 }
 private final ConnectivityManager.NetworkCallback networks=new ConnectivityManager.NetworkCallback() {
  @Override public void onAvailable(Network network) {enqueue(()->{underlay=network;links=null;capabilities=null;blocked=false;});}
  @Override public void onCapabilitiesChanged(Network network,NetworkCapabilities value) {enqueue(()->{if(network.equals(underlay)){capabilities=value;observe();}});}
  @Override public void onLinkPropertiesChanged(Network network,LinkProperties value) {enqueue(()->{if(network.equals(underlay)){links=value;observe();}});}
  @Override public void onBlockedStatusChanged(Network network,boolean value) {enqueue(()->{if(network.equals(underlay)){blocked=value;observe();}});}
  @Override public void onLost(Network network) {enqueue(()->{if(network.equals(underlay)){underlay=null;links=null;capabilities=null;lifecycle.observeUnderlay("");reconcile();}});}
 };
 @Override public void onCreate(){super.onCreate();connectivity=getSystemService(ConnectivityManager.class);}
 @Override public int onStartCommand(Intent intent,int flags,int id) {
  String action=intent==null?"":intent.getAction();
  if("stop".equals(action)){enqueue(()->{state="未连接";finish();});return START_NOT_STICKY;}
  if("tailscale-login".equals(action)){enqueue(()->{
   if(core==null||!settingUp)return;
   try{tailscaleStatus=core.beginTailscaleLogin("tailscale-"+setupLine);setupMessage="请打开浏览器完成登录";}
   catch(Exception error){setupMessage="暂未取得登录地址，请检查网络后重试";}
  });return START_NOT_STICKY;}
  if("tailscale-logout".equals(action)){enqueue(()->{
   if(core==null||!settingUp)return;
   try{core.tailscaleLogout("tailscale-"+setupLine);tailscaleStatus="";setupMessage="已退出登录";}
   catch(Exception error){setupMessage="退出登录未完成，请检查网络后重试";}
  });return START_NOT_STICKY;}
  if("tailscale-select".equals(action)){final String node=intent.getStringExtra("node");enqueue(()->{
   if(core==null||!settingUp)return;
   try{
    String fresh=core.tailscaleStatus("tailscale-"+setupLine);
    ProfileStore store=new ProfileStore(this);
    String updated=Libbox.selectTailscaleExit(store.load(),setupLine,fresh,node);
    // Retire the setup identity owner before publishing a data-session binding.
    stopCore();store.save(updated);MobileSelection.invalidate();state="Tailscale 场景已保存，可拨动拨盘连接";finish();
   }catch(Exception error){setupMessage="节点暂不可用或无法保存，请刷新后重试";if(core==null){state="无法保存场景，原配置保留";finish();}}
  });return START_NOT_STICKY;}
  if("probe".equals(action)){
   enqueue(()->{if(core!=null){final long epoch=coreEpoch;probes.execute(()->{
    String result=ConnectionProbe.run(connectivity);
    enqueue(()->{if(core!=null && coreEpoch==epoch){probeResult=result;android.util.Log.i("XDialProbe",result);}});
   });}});return START_NOT_STICKY;
  }
  if("identity-test".equals(action)){
   if((getApplicationInfo().flags&android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE)!=0)enqueue(()->{
    try{probeResult=IdentityStore.verify(this);}catch(Exception error){probeResult="安全存储验收失败";}
    android.util.Log.i("XDialProbe",probeResult);
   });return START_NOT_STICKY;
  }
  if("diagnose".equals(action)){enqueue(this::diagnose);return START_NOT_STICKY;}
  NotificationManager nm=getSystemService(NotificationManager.class);
  nm.createNotificationChannel(new NotificationChannel("connection","连接状态",NotificationManager.IMPORTANCE_LOW));
  PendingIntent open=PendingIntent.getActivity(this,0,new Intent(this,MainActivity.class),PendingIntent.FLAG_IMMUTABLE);
  PendingIntent stop=PendingIntent.getService(this,1,new Intent(this,TunnelService.class).setAction("stop"),PendingIntent.FLAG_IMMUTABLE);
  startForeground(1,new Notification.Builder(this,"connection").setSmallIcon(android.R.drawable.stat_sys_upload_done)
   .setContentTitle("XDial").setContentText("连接服务运行中 · 可随时断开").setContentIntent(open).setOngoing(true)
   .addAction(new Notification.Action.Builder(null,"断开",stop).build()).build());
  enqueue(()->{
   if(active)return;
   settingUp="tailscale-setup".equals(action);setupLine=settingUp?intent.getStringExtra("line"):"";tailscaleStatus="";setupMessage="";
   active=true;probeResult="";lifecycle.requestConnection(true);state="等待可用网络";
   try {
    Network current=connectivity.getActiveNetwork();NetworkCapabilities caps=current==null?null:connectivity.getNetworkCapabilities(current);
    if(caps!=null && caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN))throw new IllegalStateException("Existing VPN");
    // Android selects its best matching network. No interface/transport ranking.
    connectivity.requestNetwork(new NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
     .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(),networks);
    watching=true;
   }catch(Exception error){state="无法取得系统网络，请检查 VPN 状态";finish();}
  });
  return START_NOT_STICKY;
 }
 private void observe(){
  if(underlay==null || links==null || capabilities==null)return;
  boolean usable=!blocked && links.getInterfaceName()!=null && !links.getLinkAddresses().isEmpty() && !links.getDnsServers().isEmpty()
   && capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) && !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN);
  // LinkProperties equality includes DNS, addresses and routes; identity stays
  // process-local and is never emitted to logs or sent as product telemetry.
  lifecycle.observeUnderlay(usable?underlay.toString()+":"+links.toString():"");reconcile();
 }
 private void reconcile(){
  if(destroyed || !active)return;
  try {
   JSONObject snapshot=new JSONObject(lifecycle.snapshot());long epoch=snapshot.getLong("epoch");
   if(core!=null && coreEpoch!=epoch)stopCore();
   String phase=snapshot.getString("phase");
   if(phase.equals("waiting_network")){probeResult="";state="网络已断开，等待恢复";setUnderlyingNetworks(new Network[0]);return;}
   if(phase.equals("failed")){finish();return;}
   long attempt=lifecycle.nextAttempt(SystemClock.elapsedRealtime());
   if(attempt!=0)connect(attempt);
  }catch(Exception error){state="连接状态异常，请重新连接";finish();}
 }
 private void connect(final long epoch){
  state="正在连接";probeResult="";coreEpoch=epoch;
  try {
   stage="配置读取";
   String ssid="";
   if(checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION)==android.content.pm.PackageManager.PERMISSION_GRANTED) {
    android.net.wifi.WifiManager wifi=(android.net.wifi.WifiManager)getApplicationContext().getSystemService(WIFI_SERVICE);
    android.net.wifi.WifiInfo info=wifi.getConnectionInfo();
    if(info!=null && !android.net.wifi.WifiManager.UNKNOWN_SSID.equals(info.getSSID())) {
     ssid=info.getSSID();if(ssid.startsWith("\"") && ssid.endsWith("\"")) ssid=ssid.substring(1,ssid.length()-1);
    }
   }
   if(tunnel==null&&!settingUp)MobileSelection.get(this).observeBeforeConnect(ssid);
   String profile=new ProfileStore(this).load();
   stage="系统网络";
   final Network captured=underlay;
   if(captured==null || links==null || capabilities==null)throw new java.io.IOException("Underlay unavailable");
   NetworkInterface nic=NetworkInterface.getByName(links.getInterfaceName());
   if(nic==null)throw new java.io.IOException("Interface unavailable");
   stage="配置编译";
   JSONArray systemDNS = new JSONArray();
   for(java.net.InetAddress server : links.getDnsServers()) systemDNS.put(server.getHostAddress());
   String config=settingUp?Libbox.generateAndroidTailscaleSetupConfig(profile,setupLine,systemDNS.toString(),getNoBackupFilesDir().getAbsolutePath()):Libbox.generateAndroidConfig(profile,systemDNS.toString(),getNoBackupFilesDir().getAbsolutePath());
   stage="核心初始化";
   core=Libbox.new_(new Callback(){
    public void onStatusChanged(String json){}
    public void onError(long code,String message){enqueue(()->{
     if(core==null || !active || coreEpoch!=epoch)return;
     state="连接错误（"+code+"），请重新连接";
     lifecycle.completeAttempt(epoch,false,false,SystemClock.elapsedRealtime());reconcile();
    });}
   });
   core.setSecureStateStore(new IdentityStore(this));
   core.setSocketProtector(fd->{
    try{
     if(!protect((int)fd))throw new IllegalStateException("Socket protection denied");
     try(ParcelFileDescriptor duplicate=ParcelFileDescriptor.fromFd((int)fd)){captured.bindSocket(duplicate.getFileDescriptor());}
     protectedSockets.incrementAndGet();
    }catch(Exception error){protectionFailures.incrementAndGet();throw error;}
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
   if(settingUp){
    stage="准备 Tailscale 登录";core.startStandalone(config);
    lifecycle.completeAttempt(epoch,true,false,SystemClock.elapsedRealtime());state="Tailscale 账号设置（未接管流量）";
    refreshTailscale(epoch);return;
   }
   JSONObject generated=new JSONObject(config);
   JSONArray inbounds=generated.getJSONArray("inbounds"); JSONObject tun=null;
   for(int i=0;i<inbounds.length();i++) if(inbounds.getJSONObject(i).getString("type").equals("tun")) tun=inbounds.getJSONObject(i);
   if(tun==null) throw new IllegalStateException("No TUN");
   Builder builder=new Builder().setSession("XDial").setMtu(tun.getInt("mtu"));
   JSONArray addresses=tun.getJSONArray("address");
   for(int i=0;i<addresses.length();i++) { String[] cidr=addresses.getString(i).split("/"); builder.addAddress(cidr[0],Integer.parseInt(cidr[1])); }
   builder.addRoute("0.0.0.0",0).addRoute("::",0).addDnsServer("198.18.0.2");
   builder.setUnderlyingNetworks(new Network[]{captured});
   stage="建立 TUN";
   // Keep the OS TUN across unavailable-network intervals. Stop its sole reader
   // before starting the next core; never run two gVisor stacks on the same fd.
   if(tunnel==null)tunnel=builder.establish();
   if(tunnel==null)throw new IllegalStateException("VPN permission revoked");
   setUnderlyingNetworks(new Network[]{captured});core.setTunFD(tunnel.getFd());
   stage="启动数据面";core.startAndroidSession(profile,config,systemDNS.toString());
   awaitReady(epoch,profile,SystemClock.elapsedRealtime()+30000);
  }catch(Exception error){
   stopCore();
   lifecycle.completeAttempt(epoch,false,error instanceof java.io.IOException,SystemClock.elapsedRealtime());
   try{
    JSONObject result=new JSONObject(lifecycle.snapshot());
    if(result.getString("phase").equals("backoff")){
     state="网络暂不可用，正在重试";
     if(retry!=null)retry.cancel(false);
     retry=worker.schedule(this::reconcile,Math.max(1,result.getLong("retry_at_ms")-SystemClock.elapsedRealtime()),TimeUnit.MILLISECONDS);
    }else{state=stage+"失败（"+error.getClass().getSimpleName()+"）";finish();}
   }catch(Exception ignored){finish();}
  }
 }
 private void refreshTailscale(long epoch){
  if(core==null||!settingUp||coreEpoch!=epoch)return;
  try{tailscaleStatus=core.tailscaleStatus("tailscale-"+setupLine);}
  catch(Exception error){tailscaleStatus="";setupMessage="暂时无法读取账号状态";}
  statusRefresh=worker.schedule(()->refreshTailscale(epoch),2000,TimeUnit.MILLISECONDS);
 }
 private void awaitReady(long epoch,String profile,long deadline){
  if(core==null||coreEpoch!=epoch||!active)return;
  try{
   if(core.androidSessionReady(profile)){
    MobileSelection.get(this).connected(true);lifecycle.completeAttempt(epoch,true,false,SystemClock.elapsedRealtime());state="已连接";return;
   }
   if(SystemClock.elapsedRealtime()>=deadline)throw new java.io.IOException("Exit readiness timeout");
   state="正在验证出口连接";statusRefresh=worker.schedule(()->awaitReady(epoch,profile,deadline),1000,TimeUnit.MILLISECONDS);
  }catch(Exception error){state="出口未就绪，请检查 Tailscale 登录、授权和节点状态";finish();}
 }
 private void diagnose(){
  try{
   JSONObject facts=core==null?new JSONObject():new JSONObject(core.diagnostics());JSONObject safe=new JSONObject();
   for(String key:new String[]{"running","gvisor_compiled","tun_fd_ready"})safe.put(key,facts.opt(key));
   if(core!=null)safe.put("routed_traffic",new JSONObject(core.trafficTotals()));
   safe.put("session_kind",settingUp?"setup":"traffic");safe.put("protected_sockets",protectedSockets.get());safe.put("protection_failures",protectionFailures.get());
   safe.put("lifecycle",new JSONObject(lifecycle.snapshot()));android.util.Log.i("XDialDiagnostics",safe.toString());
  }catch(Exception ignored){}
 }
 private void stopCore(){
  if(statusRefresh!=null){statusRefresh.cancel(false);statusRefresh=null;}
  tailscaleStatus="";
  if(core!=null){Libbox_ previous=core;core=null;try{previous.stop();}catch(Exception ignored){}}
  try{MobileSelection.get(this).connected(false);}catch(Exception ignored){}
 }
 private void cleanup(){
  lifecycle.requestConnection(false);if(retry!=null){retry.cancel(false);retry=null;}
  if(watching){try{connectivity.unregisterNetworkCallback(networks);}catch(Exception ignored){}watching=false;}
  stopCore();
  if(tunnel!=null){try{tunnel.close();}catch(Exception ignored){}tunnel=null;}
  underlay=null;links=null;capabilities=null;active=false;settingUp=false;setupLine="";tailscaleStatus="";setupMessage="";probeResult="";
 }
 private void finish(){cleanup();stopForeground(STOP_FOREGROUND_REMOVE);stopSelf();}
 @Override public void onRevoke(){enqueue(()->{state="VPN 授权已撤销";finish();});}
 @Override public void onDestroy(){destroyed=true;probes.shutdownNow();worker.execute(this::cleanup);worker.shutdown();super.onDestroy();}
}
