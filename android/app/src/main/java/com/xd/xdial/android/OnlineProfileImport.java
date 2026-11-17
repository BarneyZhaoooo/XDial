package com.xd.xdial.android;

import android.content.Context;
import android.content.Intent;
import android.os.Handler;
import android.os.Looper;
import libbox.Libbox_;

/** One in-memory request. URLs/previews never enter Intents, logs or Bundles. */
final class OnlineProfileImport {
 interface Listener {void finished(String preview);}
 private static OnlineProfileImport pending;
 final String id=java.util.UUID.randomUUID().toString();
 final String library,url,line,name,source;
 volatile boolean cancelled;
 volatile Libbox_ runtime;
 private Listener listener;
 private boolean done;
 String result="";
 private OnlineProfileImport(String library,String url,String line,String name,String source,Listener listener){this.library=library;this.url=url;this.line=line;this.name=name;this.source=source;this.listener=listener;}
 static synchronized OnlineProfileImport begin(String library,String url,String line,String name,String source,Listener listener)throws Exception{
  if(pending!=null||TunnelService.active)throw new IllegalStateException("Import unavailable");
  pending=new OnlineProfileImport(library,url,line,name,source,listener);return pending;
 }
 static synchronized OnlineProfileImport find(String id){return pending!=null&&pending.id.equals(id)?pending:null;}
 void start(Context context){context.startForegroundService(new Intent(context,TunnelService.class).setAction("import-profile").putExtra("request",id));}
 void cancel(Context context){
  cancelled=true;Libbox_ current=runtime;if(current!=null)current.cancelPreparation();
  try{context.startService(new Intent(context,TunnelService.class).setAction("cancel-import").putExtra("request",id));}catch(RuntimeException ignored){}
  complete();
 }
 synchronized void detach(){listener=null;}
 void complete(){
  final Listener target;final String preview;
  synchronized(this){if(done)return;done=true;target=listener;listener=null;runtime=null;preview=cancelled?"":result;result="";}
  synchronized(OnlineProfileImport.class){if(pending==this)pending=null;}
  if(target!=null)new Handler(Looper.getMainLooper()).post(()->target.finished(cancelled?"":preview));
 }
}
