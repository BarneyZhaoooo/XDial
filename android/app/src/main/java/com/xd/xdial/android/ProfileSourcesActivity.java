package com.xd.xdial.android;

import android.app.*;
import android.content.Intent;
import android.os.Bundle;
import android.view.*;
import android.widget.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.*;
import org.json.*;
import libbox.Libbox;

/** Native document selection and encrypted persistence; all library ownership,
 * import validation and stale-preview checks are shared Go operations. */
public final class ProfileSourcesActivity extends Activity {
 private static final int PICK_PROFILE=42, APPROVE_DOWNLOAD=43, MAX_BYTES=2*1024*1024;
 private final ExecutorService files=Executors.newSingleThreadExecutor();
 private LinearLayout rows;
 private TextView status;
 private boolean busy;
 private OnlineProfileImport onlineRequest;
 private int onlineGeneration;
 private String pendingID="",pendingName="",preview="";

 @Override public void onCreate(Bundle saved){
  super.onCreate(saved);getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
  if(saved!=null){pendingID=saved.getString("source","");pendingName=saved.getString("name","");}
  ScrollView scroll=new ScrollView(this);LinearLayout layout=new LinearLayout(this);layout.setOrientation(1);layout.setPadding(dp(24),dp(16),dp(24),dp(24));scroll.addView(layout);
  scroll.setOnApplyWindowInsetsListener((view,insets)->{
   if(android.os.Build.VERSION.SDK_INT>=30){android.graphics.Insets bars=insets.getInsets(WindowInsets.Type.systemBars());view.setPadding(bars.left,bars.top,bars.right,bars.bottom);}
   else view.setPadding(insets.getSystemWindowInsetLeft(),insets.getSystemWindowInsetTop(),insets.getSystemWindowInsetRight(),insets.getSystemWindowInsetBottom());return insets;
  });
  button(layout,"返回连接",()->finish());
  TextView title=new TextView(this);title.setText("配置来源");title.setTextSize(24);layout.addView(title);
  TextView hint=new TextView(this);hint.setText("从 XDial 或 sing-box JSON 文件导入线路与规则，最多 2 MB。场景独立保存；更新来源保留你的场景和出口选择。文件导入不会联网；链接下载使用你指定的线路，不接管应用流量，也不会下载配置中声明的规则。");layout.addView(hint);
  button(layout,"添加配置来源",()->edit("",""));
  button(layout,"从链接添加来源",this::onlineDialog);
  button(layout,"取消下载",()->{onlineGeneration++;if(onlineRequest!=null)onlineRequest.cancel(this);});
  status=new TextView(this);status.setPadding(0,dp(16),0,dp(16));layout.addView(status);
  rows=new LinearLayout(this);rows.setOrientation(1);layout.addView(rows);setContentView(scroll);
 }
 private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
 private void button(LinearLayout parent,String text,Runnable action){Button b=new Button(this);b.setAllCaps(false);b.setText(text);b.setOnClickListener(v->action.run());parent.addView(b);}
 @Override protected void onResume(){super.onResume();if(!busy)reload("");}
 @Override protected void onSaveInstanceState(Bundle out){super.onSaveInstanceState(out);out.putString("source",pendingID);out.putString("name",pendingName);}
 @Override protected void onDestroy(){onlineGeneration++;if(onlineRequest!=null){onlineRequest.detach();onlineRequest.cancel(this);onlineRequest=null;}preview="";files.shutdownNow();super.onDestroy();}
 private boolean canEdit(){
  if(busy||TunnelService.active){Toast.makeText(this,TunnelService.active?"请先断开连接再修改配置来源":"正在处理，请稍候",Toast.LENGTH_SHORT).show();return false;}return true;
 }
 private void reload(String message){
  busy=true;status.setText("正在读取配置来源…");rows.removeAllViews();
  files.execute(()->{
   try{JSONObject summary=new JSONObject(new ProfileStore(this).summary());runOnUiThread(()->render(summary,message));}
   catch(Exception e){runOnUiThread(()->{if(isDestroyed())return;busy=false;status.setText("无法解密或读取配置库。原文件保留，请检查本机存储后重试。");});}
  });
 }
 private void render(JSONObject summary,String message){
  if(isDestroyed())return;busy=false;rows.removeAllViews();
  status.setText(!message.isEmpty()?message:TunnelService.active?"连接期间仅可查看来源。":"已保存 "+summary.optInt("scenarios")+" 个全局场景。选择编辑来源不会切换场景。");
  JSONArray sources=summary.optJSONArray("sources");if(sources==null)return;
  for(int i=0;i<sources.length();i++){
   JSONObject source=sources.optJSONObject(i);if(source==null)continue;
   String id=source.optString("id"),name=source.optString("name");
   button(rows,name+(id.equals(summary.optString("editing_id"))?" · 当前编辑":"")+"\n"+source.optInt("lines")+" 条线路 · "+source.optInt("rules")+" 份规则",()->choose(id,name,source.optBoolean("remote")));
  }
 }
 private void choose(String id,String name,boolean remote){
  if(!canEdit())return;
  new AlertDialog.Builder(this).setTitle(name).setItems(new String[]{"设为编辑来源",remote?"从原链接更新":"从新文件更新","移除来源"},(d,index)->{
   if(index==0)mutate(()->new ProfileStore(this).selectSource(id),"编辑来源已更改，当前场景保持不变。","无法切换编辑来源，请重试。");
   else if(index==1){if(remote)refreshOnline(id,name);else edit(id,name);}else new AlertDialog.Builder(this).setTitle("移除配置来源？").setMessage("全局场景保持不变。若场景或线路组仍引用此来源的资源，将保留来源；至少保留一份来源。")
    .setNegativeButton("取消",null).setPositiveButton("移除",(dialog,which)->{if(canEdit())mutate(()->new ProfileStore(this).removeSource(id),"来源已移除，场景保持不变。","无法移除：仍有引用或已是最后一份来源。原配置保留。");}).show();
  }).setNegativeButton("取消",null).show();
 }
 private EditText input(String hint){
  EditText field=new EditText(this);field.setHint(hint);field.setSaveEnabled(false);field.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
  field.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);return field;
 }
 private void edit(String id,String existingName){
  if(!canEdit())return;
  LinearLayout fields=new LinearLayout(this);fields.setOrientation(1);fields.setPadding(dp(24),0,dp(24),0);
  EditText name=input("来源名称");name.setSingleLine(true);name.setText(existingName);name.setEnabled(id.isEmpty());fields.addView(name);
  EditText content=input("粘贴 JSON，或选择文件");content.setInputType(content.getInputType()|android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE);content.setMinLines(4);content.setMaxLines(8);content.setGravity(Gravity.TOP);fields.addView(content);
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle(id.isEmpty()?"添加来源":"更新来源").setView(fields).setNegativeButton("取消",null).setNeutralButton("选择文件",null).setPositiveButton("预览",null).create();
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
   dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setOnClickListener(v->{
    if(!canEdit()||!hasName(name))return;pendingID=id;pendingName=name.getText().toString().trim();content.setText("");dialog.dismiss();
    Intent pick=new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
    try{startActivityForResult(pick,PICK_PROFILE);}catch(Exception e){pendingID="";pendingName="";status.setText("系统文件选择器不可用，请稍后重试或粘贴内容。");}
   });
   dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{
    if(!canEdit()||!hasName(name))return;String text=content.getText().toString(),label=name.getText().toString().trim();content.setText("");dialog.dismiss();prepare(()->text,label,id);
   });
  });dialog.setOnDismissListener(d->content.setText(""));dialog.show();
 }
 private boolean hasName(EditText name){if(name.getText().toString().trim().isEmpty()){name.setError("请填写来源名称");return false;}return true;}
 @Override protected void onActivityResult(int request,int result,Intent data){
  super.onActivityResult(request,result,data);
  if(request==APPROVE_DOWNLOAD){if(onlineRequest!=null){if(result==RESULT_OK)onlineRequest.start(this);else onlineRequest.cancel(this);}return;}
  if(request!=PICK_PROFILE)return;
  final String id=pendingID,name=pendingName;pendingID="";pendingName="";
  if(result!=RESULT_OK||data==null||data.getData()==null||name.isEmpty())return;
  if(!canEdit())return;final android.net.Uri uri=data.getData();
  prepare(()->{
   try(InputStream stream=getContentResolver().openInputStream(uri);ByteArrayOutputStream output=new ByteArrayOutputStream()){
    if(stream==null)throw new IOException("Missing document");byte[] buffer=new byte[8192];int size;
    while((size=stream.read(buffer))!=-1){if(Thread.currentThread().isInterrupted()||output.size()+size>MAX_BYTES)throw new IOException("Document exceeds limit");output.write(buffer,0,size);}return output.toString(StandardCharsets.UTF_8.name());
   }
  },name,id);
 }

 private void onlineDialog(){
  if(!canEdit())return;
  LinearLayout fields=new LinearLayout(this);fields.setOrientation(1);fields.setPadding(dp(24),0,dp(24),0);
  EditText name=input("来源名称"),url=input("HTTPS 配置链接");name.setSingleLine(true);url.setSingleLine(true);fields.addView(name);fields.addView(url);
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle("从链接添加来源").setView(fields).setNegativeButton("取消",null).setPositiveButton("选择获取线路",null).create();
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{
   if(!hasName(name)||!canEdit())return;
   try{
    String address=url.getText().toString().trim(),label=name.getText().toString().trim();Libbox.validateProfileSourceURL(address);
    JSONArray lines=new JSONObject(Libbox.scenarioEditor(new ProfileStore(this).load(),"")).getJSONArray("lines");
    java.util.ArrayList<String> names=new java.util.ArrayList<>(),ids=new java.util.ArrayList<>();
    for(int i=0;i<lines.length();i++){JSONObject line=lines.getJSONObject(i);if(line.optBoolean("enabled")){ids.add(line.getString("id"));names.add(line.optString("name",line.getString("id")));}}
    dialog.dismiss();new AlertDialog.Builder(this).setTitle("下载使用的线路").setItems(names.toArray(new String[0]),(p,w)->startOnline(address,ids.get(w),label,"")).setNegativeButton("取消",null).show();
   }catch(Exception e){url.setError("请填写有效的公开 HTTPS 配置链接，不能包含用户名、密码或片段。");}
  });});dialog.show();
 }
 private void refreshOnline(String id,String name){
  try{JSONObject source=new JSONObject(new ProfileStore(this).remoteSource(id));startOnline(source.getString("url"),source.getString("fetchLineID"),name,id);}
  catch(Exception e){status.setText("来源信息不可用，原配置保留。请重新添加来源。");}
 }
 private void startOnline(String url,String line,String name,String id){
  if(!canEdit())return;busy=true;status.setText("正在准备下载；尚未修改配置…");final int generation=++onlineGeneration;
  files.execute(()->{
   try{String library=new ProfileStore(this).librarySnapshot();runOnUiThread(()->{
    if(isDestroyed()||generation!=onlineGeneration){if(!isDestroyed())reload("已取消下载。");return;}
    try{
     onlineRequest=OnlineProfileImport.begin(library,url,line,name,id,result->{
      if(isDestroyed())return;boolean cancelled=onlineRequest!=null&&onlineRequest.cancelled;onlineRequest=null;
      if(result.isEmpty())reload(cancelled?"已取消下载，原配置保留。":"下载或校验未完成：请检查网络、链接、获取线路及 JSON 格式。原配置保留。");else showPreview(result);
     });
     Intent consent=android.net.VpnService.prepare(this);
     if(consent!=null){status.setText("需要系统授权，才能让配置下载绕开自身 VPN；本次不会接管流量。");startActivityForResult(consent,APPROVE_DOWNLOAD);}
     else onlineRequest.start(this);
    }catch(Exception e){if(onlineRequest!=null){onlineRequest.cancel(this);onlineRequest=null;}else reload("下载服务未能启动，原配置保留。请重试。");}
   });}catch(Exception e){runOnUiThread(()->{if(!isDestroyed())reload("无法读取下载所需配置，原配置保留。");});}
  });
 }
 private interface Content{String read()throws Exception;}
 private void prepare(Content content,String name,String id){
  busy=true;status.setText("正在校验配置并生成预览…");rows.removeAllViews();
  files.execute(()->{
   try{
    if(TunnelService.active)throw new IOException("Session started");String result=new ProfileStore(this).preview(content.read(),name,id);
    runOnUiThread(()->showPreview(result));
   }catch(Exception e){runOnUiThread(()->{if(!isDestroyed())reload("无法导入：请检查 JSON 格式、2 MB 大小限制、资源引用和 SSID 是否冲突。更新来源不能移除正在使用的线路或规则。原配置保留。");});}
  });
 }
 private void showPreview(String result){
  if(isDestroyed())return;busy=false;
  try{
   JSONObject value=new JSONObject(result);preview=result;
   String message=value.optInt("lines")+" 条线路 · "+value.optInt("rules")+" 份规则\n"+(value.optBoolean("updating")?"更新来源资源，保留所有全局场景。":"添加 "+value.optInt("scenarios")+" 个场景模板，当前场景保持不变。");
   int warnings=value.optInt("warnings");if(warnings>0)message+="\n有 "+warnings+" 项不受支持的内容；请修正来源配置后重新导入。";
   AlertDialog.Builder builder=new AlertDialog.Builder(this).setTitle("预览 · "+value.optString("name")).setMessage(message).setNegativeButton("取消",null);
   // Unexecutable imported policy is never silently accepted by the mobile UI.
   if(warnings==0)builder.setPositiveButton("保存",(d,w)->{
    String candidate=preview;preview="";if(canEdit())mutate(()->new ProfileStore(this).commitPreview(candidate),"来源已保存，当前场景保持不变。","配置已变化或保存失败，请重新预览。原配置保留。");
   });
   AlertDialog dialog=builder.create();dialog.setOnDismissListener(d->{preview="";if(!busy&&!isDestroyed())reload("");});dialog.show();dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
  }catch(Exception e){preview="";reload("预览读取失败，原配置保留。请重新导入。");}
 }
 private interface Mutation{void run()throws Exception;}
 private void mutate(Mutation action,String success,String failure){
  busy=true;status.setText("正在保存配置来源…");rows.removeAllViews();
  files.execute(()->{
   String message=success;try{if(TunnelService.active)throw new IOException("Session started");action.run();MobileSelection.invalidate();}catch(Exception e){message=failure;}
   final String result=message;runOnUiThread(()->{if(!isDestroyed())reload(result);});
  });
 }
}
