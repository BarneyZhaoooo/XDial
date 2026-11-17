package com.xd.xdial.android;

import android.app.*;
import android.os.Bundle;
import android.view.*;
import android.widget.*;
import libbox.Libbox;
import org.json.*;

/** Native draft editor. Go owns edits and validation; Android owns presentation,
 * cancellation and the encrypted library CAS. Drafts never enter saved bundles. */
public final class ScenariosActivity extends Activity {
 private String original="",draft="",editing="";
 private LinearLayout content;
 private TextView status;
 private static final class Draft {final String original,text,id;Draft(String a,String b,String c){original=a;text=b;id=c;}}
 @Override public void onCreate(Bundle state){
  super.onCreate(state);if(android.os.Build.VERSION.SDK_INT>=33)getOnBackInvokedDispatcher().registerOnBackInvokedCallback(android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT,this::leave);getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
  ScrollView scroll=new ScrollView(this);LinearLayout page=new LinearLayout(this);page.setOrientation(1);page.setPadding(dp(24),dp(16),dp(24),dp(24));scroll.addView(page);
  scroll.setOnApplyWindowInsetsListener((v,insets)->{if(android.os.Build.VERSION.SDK_INT>=30){android.graphics.Insets b=insets.getInsets(WindowInsets.Type.systemBars());v.setPadding(b.left,b.top,b.right,b.bottom);}else v.setPadding(insets.getSystemWindowInsetLeft(),insets.getSystemWindowInsetTop(),insets.getSystemWindowInsetRight(),insets.getSystemWindowInsetBottom());return insets;});
  button(page,"返回连接",this::leave);TextView title=new TextView(this);title.setText("场景编辑");title.setTextSize(24);page.addView(title);
  status=new TextView(this);page.addView(status);content=new LinearLayout(this);content.setOrientation(1);page.addView(content);setContentView(scroll);
  try{Draft retained=(Draft)getLastNonConfigurationInstance();if(retained!=null){original=retained.original;draft=retained.text;editing=retained.id;}else{original=new ProfileStore(this).load();draft=original;}render();}
  catch(Exception error){status.setText("无法读取配置库，请先导入配置后重试。");}
 }
 @Override public Object onRetainNonConfigurationInstance(){return new Draft(original,draft,editing);}
 @Override public void onBackPressed(){leave();}
 private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
 private void button(LinearLayout parent,String text,Runnable action){Button b=new Button(this);b.setAllCaps(false);b.setText(text);b.setOnClickListener(v->action.run());parent.addView(b);}
 private void note(String text){TextView v=new TextView(this);v.setText(text);v.setPadding(0,dp(8),0,dp(8));content.addView(v);}
 private boolean editable(){if(TunnelService.active){status.setText("连接期间只可查看；请先断开再编辑和保存。");return false;}return true;}
 private void leave(){if(!draft.equals(original))new AlertDialog.Builder(this).setTitle("放弃未保存的场景修改？").setNegativeButton("继续编辑",null).setPositiveButton("放弃",(d,w)->finish()).show();else finish();}
 private JSONObject command(String action)throws Exception{return new JSONObject().put("action",action).put("id",editing);}
 private void apply(JSONObject command){
  if(!editable())return;
  try{draft=Libbox.editScenario(draft,command.toString());if(command.optString("action").equals("create")){JSONArray scenes=new JSONObject(Libbox.scenarioEditor(draft,"")).getJSONArray("scenarios");editing=scenes.getJSONObject(scenes.length()-1).getString("id");}else if(command.optString("action").equals("delete"))editing="";render();}
  catch(Exception error){status.setText("无法修改：请检查名称、可用出口和规则引用。删除前请切换到其它场景，并清除它的自动条件；调整顺序前请改用列表顺序。草稿保留。");}
 }
 private interface Value {void accept(String value)throws Exception;}
 private EditText field(String hint,String value){EditText e=new EditText(this);e.setHint(hint);e.setText(value);e.setSaveEnabled(false);e.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);e.setInputType(android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);return e;}
 private void textDialog(String title,String initial,Value done){
  if(!editable())return;EditText name=field("场景名称",initial);name.setSingleLine(true);
  AlertDialog dialog=new AlertDialog.Builder(this).setTitle(title).setView(name).setNegativeButton("取消",null).setPositiveButton("确定",null).create();
  dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{try{done.accept(name.getText().toString());dialog.dismiss();}catch(Exception e){name.setError("无法读取选择，请重新打开编辑器");}});});dialog.show();
 }
 private void chooseLine(JSONObject view,Value done){
  try{JSONArray all=view.getJSONArray("lines");java.util.ArrayList<String> names=new java.util.ArrayList<>(),ids=new java.util.ArrayList<>();
   for(int i=0;i<all.length();i++){JSONObject item=all.getJSONObject(i);if(item.getBoolean("enabled")){ids.add(item.getString("id"));names.add(label(item));}}
   new AlertDialog.Builder(this).setTitle("选择出口").setItems(names.toArray(new String[0]),(d,w)->{try{done.accept(ids.get(w));}catch(Exception e){status.setText("无法修改出口，请重新打开编辑器。");}}).setNegativeButton("取消",null).show();
  }catch(Exception error){status.setText("无法读取线路。");}
 }
 private String label(JSONObject item){String name=item.optString("name");return name.isEmpty()?item.optString("id"):name;}
 private String label(JSONArray items,String id){for(int i=0;i<items.length();i++){JSONObject item=items.optJSONObject(i);if(item!=null&&id.equals(item.optString("id")))return label(item)+(item.optBoolean("enabled",true)?"":"（已停用）");}return "出口或规则不可用";}
 private void render()throws Exception{
  content.removeAllViews();JSONObject view=new JSONObject(Libbox.scenarioEditor(draft,editing));
  status.setText(TunnelService.active?"连接期间只可查看。":draft.equals(original)?"修改只影响场景，不改变来源线路和规则。":"草稿尚未保存；当前连接场景保持不变。");
  button(content,"保存修改",()->{if(!editable()||draft.equals(original))return;try{MobileSelection.saveIdleScenario(this,draft);original=new ProfileStore(this).load();draft=original;render();status.setText("场景已保存，当前选择保持不变。");}catch(Exception error){status.setText("保存失败或配置已变化。草稿保留，请返回后重新打开；原配置不会被覆盖。");}});
  button(content,"新建场景",()->textDialog("新建场景","",name->chooseLine(view,line->apply(new JSONObject().put("action","create").put("name",name).put("line_id",line)))));
  JSONArray scenes=view.getJSONArray("scenarios"),lines=view.getJSONArray("lines"),rules=view.getJSONArray("rules");
  for(int i=0;i<scenes.length();i++){JSONObject item=scenes.getJSONObject(i);String id=item.getString("id");button(content,label(item)+(id.equals(view.getString("selected"))?" · 当前选择":"")+(id.equals(editing)?" · 正在编辑":""),()->{editing=id;try{render();}catch(Exception e){status.setText("无法读取场景，请重新打开。");}});}
  JSONObject scene=view.optJSONObject("scenario");if(scene==null)return;
  note("编辑 · "+scene.getString("name"));
  button(content,"修改名称",()->textDialog("场景名称",scene.optString("name"),name->apply(command("rename").put("name",name))));
  button(content,"默认出口 · "+label(lines,scene.getString("line_id")),()->{if(editable())chooseLine(view,line->apply(command("default").put("line_id",line)));});
  button(content,"Wi-Fi 与地区条件",this::conditions);
  boolean sourceOrder=scene.getBoolean("source_order");note(sourceOrder?"沿用原配置的匹配顺序；改用列表顺序后才能调整规则位置。":"规则按下面的顺序匹配；先匹配的规则生效。");
  if(sourceOrder)button(content,"改用列表顺序",()->confirm("改用列表顺序？","这会改变交错条件的先后顺序，重叠规则的结果可能变化。部分匹配仍保持原范围。",()->{try{apply(command("list_order"));}catch(Exception ignored){}}));
  JSONArray bindings=scene.getJSONArray("bindings");java.util.HashSet<String> bound=new java.util.HashSet<>();
  for(int i=0;i<bindings.length();i++){JSONObject binding=bindings.getJSONObject(i);String rule=binding.getString("rule_id");bound.add(rule);int index=i;
   String text=(i+1)+". "+label(rules,rule)+" → "+label(lines,binding.getString("line_id"))+(binding.getInt("partial")>0?" · 部分匹配（"+binding.getInt("partial")+" 项）":"");
   button(content,text,()->bindingMenu(view,binding,index,bindings.length(),sourceOrder));
  }
  button(content,"添加规则绑定",()->{if(!editable())return;java.util.ArrayList<String> labels=new java.util.ArrayList<>(),ids=new java.util.ArrayList<>();for(int i=0;i<rules.length();i++){JSONObject rule=rules.optJSONObject(i);if(rule!=null&&rule.optBoolean("enabled")&&!bound.contains(rule.optString("id"))){ids.add(rule.optString("id"));labels.add(label(rule));}}
   if(ids.isEmpty()){status.setText("没有未绑定的可用规则；可以在配置来源中导入规则。");return;}
   new AlertDialog.Builder(this).setTitle("选择规则").setItems(labels.toArray(new String[0]),(d,w)->chooseLine(view,line->apply(command("bind").put("rule_id",ids.get(w)).put("line_id",line)))).setNegativeButton("取消",null).show();
  });
  button(content,"删除此场景",()->confirm("删除此场景？","只删除场景，不删除来源线路和规则。当前选择或仍有自动条件的场景不能删除。",()->{try{apply(command("delete"));}catch(Exception ignored){}}));
 }
 private void confirm(String title,String message,Runnable action){if(!editable())return;new AlertDialog.Builder(this).setTitle(title).setMessage(message).setNegativeButton("取消",null).setPositiveButton("确定",(d,w)->action.run()).show();}
 private void bindingMenu(JSONObject view,JSONObject binding,int index,int count,boolean sourceOrder){
  if(!editable())return;String rule=binding.optString("rule_id");java.util.ArrayList<String> labels=new java.util.ArrayList<>(),actions=new java.util.ArrayList<>();
  labels.add("修改出口");actions.add("bind");if(!sourceOrder&&index>0){labels.add("上移");actions.add("up");}if(!sourceOrder&&index+1<count){labels.add("下移");actions.add("down");}
  if(binding.optInt("partial")>0){labels.add("使用完整规则");actions.add("full_rule");}labels.add("移除绑定");actions.add("unbind");
  new AlertDialog.Builder(this).setTitle("规则绑定").setItems(labels.toArray(new String[0]),(d,w)->{
   String action=actions.get(w);try{
    if(action.equals("bind")){chooseLine(view,line->apply(command("bind").put("rule_id",rule).put("line_id",line)));return;}
    if(action.equals("full_rule")){confirm("使用完整规则？","这会增加当前未引用的匹配条件，可能影响更多流量。",()->{try{apply(command("full_rule").put("rule_id",rule));}catch(Exception ignored){}});return;}
    JSONObject change=command(action.equals("up")||action.equals("down")?"move":action).put("rule_id",rule);
    if(action.equals("up")||action.equals("down"))change.put("offset",action.equals("up")?-1:1);apply(change);
   }catch(Exception error){status.setText("无法修改规则绑定。");}
  }).setNegativeButton("取消",null).show();
 }
 private void conditions(){
  if(!editable())return;
  try{JSONObject current=new JSONObject(Libbox.scenarioConditions(draft,editing));LinearLayout fields=new LinearLayout(this);fields.setOrientation(1);fields.setPadding(dp(24),0,dp(24),0);
   EditText ssids=field("Wi-Fi 名称，每行一个",join(current.getJSONArray("ssids"),"\n"));ssids.setInputType(ssids.getInputType()|android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE);fields.addView(ssids);
   EditText countries=field("地区代码，以逗号分隔；* 表示其它地区",join(current.getJSONArray("countries"),", "));fields.addView(countries);
   AlertDialog dialog=new AlertDialog.Builder(this).setTitle("自动场景条件").setView(fields).setNegativeButton("取消",null).setPositiveButton("应用到草稿",null).create();
   dialog.setOnShowListener(d->{dialog.getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v->{if(!editable())return;try{draft=Libbox.updateScenarioConditions(draft,editing,ssids.getText().toString(),countries.getText().toString());render();dialog.dismiss();}catch(Exception error){countries.setError("请检查格式及与其它场景的条件冲突");}});});dialog.show();
  }catch(Exception error){status.setText("无法读取自动条件。");}
 }
 private String join(JSONArray values,String separator)throws Exception{StringBuilder b=new StringBuilder();for(int i=0;i<values.length();i++){if(i>0)b.append(separator);b.append(values.getString(i));}return b.toString();}
}
