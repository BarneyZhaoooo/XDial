package com.xd.xdial.android;

import android.app.*;
import android.content.Intent;
import android.os.Bundle;
import android.view.*;
import android.widget.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.*;
import libbox.Libbox;
import org.json.*;

/** Native document access only; format validation and cache identity live in Go. */
public final class OfflineRulesActivity extends Activity {
 private static final int PICK_RULE=41, MAX_BYTES=1024*1024;
 private final ExecutorService files=Executors.newSingleThreadExecutor();
 private TextView status;
 private LinearLayout rows;
 private boolean busy;
 private String pendingID="",pendingKey="";

 @Override public void onCreate(Bundle saved) {
  super.onCreate(saved);
  getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
  if(saved!=null){pendingID=saved.getString("rule","");pendingKey=saved.getString("key","");}
  ScrollView scroll=new ScrollView(this);
  LinearLayout layout=new LinearLayout(this);layout.setOrientation(1);layout.setPadding(dp(24),dp(16),dp(24),dp(24));scroll.addView(layout);
  scroll.setOnApplyWindowInsetsListener((view,insets)->{
   if(android.os.Build.VERSION.SDK_INT>=30){android.graphics.Insets bars=insets.getInsets(WindowInsets.Type.systemBars());view.setPadding(bars.left,bars.top,bars.right,bars.bottom);}
   else view.setPadding(insets.getSystemWindowInsetLeft(),insets.getSystemWindowInsetTop(),insets.getSystemWindowInsetRight(),insets.getSystemWindowInsetBottom());return insets;
  });
  Button back=new Button(this);back.setText("返回连接");back.setOnClickListener(v->finish());layout.addView(back);
  TextView title=new TextView(this);title.setText("规则与离线副本");title.setTextSize(24);layout.addView(title);
  TextView hint=new TextView(this);hint.setText("已有副本可供连接直接使用。导入只更新本机副本，不改变场景和规则来源；新内容在下次连接时使用。每份最多 1 MB。");layout.addView(hint);
  status=new TextView(this);status.setPadding(0,dp(16),0,dp(16));layout.addView(status);
  rows=new LinearLayout(this);rows.setOrientation(1);layout.addView(rows);
  setContentView(scroll);
 }
 private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
 private String root(){return getNoBackupFilesDir().getAbsolutePath();}
 @Override protected void onResume(){super.onResume();if(!busy)reload("");}
 @Override protected void onSaveInstanceState(Bundle out){super.onSaveInstanceState(out);out.putString("rule",pendingID);out.putString("key",pendingKey);}
 @Override protected void onDestroy(){files.shutdownNow();super.onDestroy();}

 private void reload(String message){
  busy=true;status.setText("正在检查本机副本…");rows.removeAllViews();
  files.execute(()->{
   try{JSONArray list=new JSONArray(Libbox.listOfflineRuleSets(new ProfileStore(this).load(),root()));runOnUiThread(()->render(list,message));}
   catch(Exception error){runOnUiThread(()->{if(isDestroyed())return;busy=false;status.setText("无法读取规则。请返回导入有效配置后重试。");});}
  });
 }
 private void render(JSONArray list,String message){
  if(isDestroyed())return;
  busy=false;rows.removeAllViews();
  status.setText(!message.isEmpty()?message:list.length()==0?"配置中没有远程规则。手动规则已随配置保存在本机。":TunnelService.active?"连接期间可查看副本；请断开后导入或移除。":"选择规则查看或导入离线副本。");
  for(int i=0;i<list.length();i++){
   JSONObject rule=list.optJSONObject(i);if(rule==null)continue;
   String state=rule.optString("state"),label=state.equals("valid")?"可离线使用":state.equals("stale")?"可离线使用 · 副本较旧":state.equals("invalid")?"副本无效，需要重新导入":"尚无离线副本";
   if(rule.optLong("updated_at")>0)label+="\n"+android.text.format.Formatter.formatFileSize(this,rule.optLong("bytes"))+" · "+java.text.DateFormat.getDateTimeInstance(java.text.DateFormat.SHORT,java.text.DateFormat.SHORT).format(new java.util.Date(rule.optLong("updated_at")*1000));
   Button button=new Button(this);button.setAllCaps(false);String name=rule.optString("name");button.setText((name.isEmpty()?rule.optString("id"):name)+"\n"+label);button.setOnClickListener(v->choose(rule));rows.addView(button);
  }
 }
 private boolean canEdit(){
  if(busy||TunnelService.active){Toast.makeText(this,TunnelService.active?"请先断开连接再修改副本":"正在处理，请稍候",Toast.LENGTH_SHORT).show();return false;}return true;
 }
 private void choose(JSONObject rule){
  if(!canEdit())return;
  boolean binary=rule.optString("format").equals("binary");
  String[] options=binary?new String[]{"从文件导入 SRS","移除本机副本"}:new String[]{"从文件导入","粘贴规则内容","移除本机副本"};
  new AlertDialog.Builder(this).setTitle(rule.optString("name","规则")).setItems(options,(d,index)->{
   if(index==0)pick(rule);else if(!binary&&index==1)paste(rule);else remove(rule);
  }).setNegativeButton("取消",null).show();
 }
 private void pick(JSONObject rule){
  if(!canEdit())return;
  pendingID=rule.optString("id");pendingKey=rule.optString("cache_key");
  Intent intent=new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
  try{startActivityForResult(intent,PICK_RULE);}catch(Exception e){pendingID="";pendingKey="";Toast.makeText(this,"系统文件选择器不可用，请稍后重试",Toast.LENGTH_LONG).show();}
 }
 private void paste(JSONObject rule){
  if(!canEdit())return;
  EditText input=new EditText(this);input.setHint(rule.optString("format").equals("text")?"域名或 CIDR 列表":"sing-box 规则 JSON");input.setMinLines(6);input.setGravity(Gravity.TOP);
  input.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);input.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle("导入规则副本").setView(input).setNegativeButton("取消",null).setPositiveButton("导入",null).create();
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{
   if(!canEdit())return;byte[] content=input.getText().toString().getBytes(StandardCharsets.UTF_8);input.setText("");dialog.dismiss();
   mutate(()->Libbox.importOfflineRuleSet(new ProfileStore(this).load(),rule.optString("id"),rule.optString("cache_key"),root(),content),"副本已校验并保存，下次连接使用。","导入失败：请检查格式、1 MB 大小限制，或重新选择规则。原副本保留。");
  });});dialog.show();
 }
 private void remove(JSONObject rule){
  new AlertDialog.Builder(this).setTitle("移除离线副本？").setMessage("规则来源和场景会保留。下次连接可能需要联网重新获取规则。")
   .setNegativeButton("取消",null).setPositiveButton("移除",(d,w)->{if(canEdit())mutate(()->Libbox.removeOfflineRuleSet(new ProfileStore(this).load(),rule.optString("id"),rule.optString("cache_key"),root()),"本机副本已移除，规则来源保留。","无法移除副本，请重试。");}).show();
 }
 @Override protected void onActivityResult(int request,int result,Intent data){
  super.onActivityResult(request,result,data);if(request!=PICK_RULE)return;
  final String id=pendingID,key=pendingKey;pendingID="";pendingKey="";
  if(result!=RESULT_OK||data==null||data.getData()==null||id.isEmpty())return;
  if(TunnelService.active){Toast.makeText(this,"连接已开始，未修改副本。请断开后重新导入。",Toast.LENGTH_LONG).show();return;}
  final android.net.Uri uri=data.getData();
  mutate(()->{
   byte[] bytes;
   try(InputStream input=getContentResolver().openInputStream(uri);ByteArrayOutputStream output=new ByteArrayOutputStream()){
    if(input==null)throw new IOException("Missing document");byte[] buffer=new byte[8192];int size;
    while((size=input.read(buffer))!=-1){if(Thread.currentThread().isInterrupted()||output.size()+size>MAX_BYTES)throw new IOException("Document exceeds limit");output.write(buffer,0,size);}bytes=output.toByteArray();
   }
   if(TunnelService.active)throw new IOException("Session started");
   Libbox.importOfflineRuleSet(new ProfileStore(this).load(),id,key,root(),bytes);
  },"副本已校验并保存，下次连接使用。","导入失败：请检查文件权限、格式、1 MB 大小限制，或重新选择规则。原副本保留。");
 }
 private interface Mutation{void run()throws Exception;}
 private void mutate(Mutation action,String success,String failure){
  busy=true;status.setText("正在处理本机副本…");rows.removeAllViews();
  files.execute(()->{
   String message=success;try{if(TunnelService.active)throw new IOException("Session started");action.run();}catch(Exception error){message=failure;}
   final String result=message;runOnUiThread(()->{if(!isDestroyed())reload(result);});
  });
 }
}
