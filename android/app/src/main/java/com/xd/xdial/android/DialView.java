package com.xd.xdial.android;

import android.animation.ValueAnimator;
import android.content.Context;
import android.graphics.*;
import android.util.TypedValue;
import android.view.*;
import android.view.accessibility.AccessibilityNodeInfo;

/** Native rotary gesture; no network or scenario decisions live in the view. */
final class DialView extends View {
 private final Paint paint=new Paint(Paint.ANTI_ALIAS_FLAG);
 private final int ink,accent;
 private float rotation,startAngle,travel;
 private boolean active;
 private ValueAnimator returning;
 DialView(Context context) {
  super(context);setClickable(true);setFocusable(true);setContentDescription("连接");
  ink=color(android.R.attr.textColorPrimary);accent=color(android.R.attr.colorAccent);
 }
 private int color(int attr) {
  android.content.res.TypedArray values=getContext().obtainStyledAttributes(new int[]{attr});
  int result=values.getColor(0,Color.GRAY);values.recycle();return result;
 }
 void setActive(boolean value) {if(active==value)return;active=value;setContentDescription(value?"断开连接":"连接");invalidate();}
 @Override protected void onDraw(Canvas canvas) {
  super.onDraw(canvas);float cx=getWidth()/2f,cy=getHeight()/2f,r=Math.min(getWidth(),getHeight())*.43f;
  paint.setStyle(Paint.Style.STROKE);paint.setColor(active?accent:ink);paint.setStrokeWidth(dp(2));paint.setAlpha(180);
  canvas.drawCircle(cx,cy,r,paint);
  canvas.save();canvas.rotate(rotation,cx,cy);
  paint.setStrokeWidth(dp(1.5f));paint.setAlpha(110);canvas.drawCircle(cx,cy,r*.53f,paint);
  for(int i=0;i<6;i++) {double angle=Math.toRadians(-150+i*52);float x=cx+(float)Math.cos(angle)*r*.76f,y=cy+(float)Math.sin(angle)*r*.76f;
   paint.setAlpha(200);paint.setStyle(Paint.Style.STROKE);canvas.drawCircle(x,y,r*.115f,paint);
  }
  canvas.restore();paint.setAlpha(255);paint.setStrokeWidth(dp(5));paint.setStrokeCap(Paint.Cap.ROUND);
  canvas.drawLine(cx+r*.81f,cy+r*.52f,cx+r*.97f,cy+r*.62f,paint);
  paint.setStyle(Paint.Style.FILL);paint.setTextAlign(Paint.Align.CENTER);paint.setTextSize(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP,25,getResources().getDisplayMetrics()));
  Paint.FontMetrics fm=paint.getFontMetrics();canvas.drawText(active?"断开":"连接",cx,cy-(fm.ascent+fm.descent)/2,paint);
 }
 private float dp(float value){return TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP,value,getResources().getDisplayMetrics());}
 private float angle(MotionEvent event){return (float)Math.toDegrees(Math.atan2(event.getY()-getHeight()/2f,event.getX()-getWidth()/2f));}
 @Override public boolean onTouchEvent(MotionEvent event) {
  if(!isEnabled())return false;
  switch(event.getActionMasked()) {
   case MotionEvent.ACTION_DOWN:
    if(returning!=null)returning.cancel();startAngle=angle(event);travel=0;getParent().requestDisallowInterceptTouchEvent(true);return true;
   case MotionEvent.ACTION_MOVE:
    float delta=angle(event)-startAngle;if(delta>180)delta-=360;if(delta< -180)delta+=360;
    int oldTick=(int)(rotation/20);rotation=Math.max(0,Math.min(110,delta));travel=Math.max(travel,Math.abs(delta));
    if((int)(rotation/20)!=oldTick)performHapticFeedback(HapticFeedbackConstants.CLOCK_TICK);invalidate();return true;
   case MotionEvent.ACTION_UP:
    if(travel<8 || rotation>=35)performClick();resetDial();return true;
   case MotionEvent.ACTION_CANCEL:resetDial();return true;
  }
  return super.onTouchEvent(event);
 }
 @Override public boolean performClick(){performHapticFeedback(HapticFeedbackConstants.CONTEXT_CLICK);super.performClick();return true;}
 private void resetDial(){getParent().requestDisallowInterceptTouchEvent(false);returning=ValueAnimator.ofFloat(rotation,0);returning.setDuration(ValueAnimator.areAnimatorsEnabled()?240:0);returning.addUpdateListener(v->{rotation=(float)v.getAnimatedValue();invalidate();});returning.start();}
 @Override protected void onDetachedFromWindow(){if(returning!=null)returning.cancel();super.onDetachedFromWindow();}
 @Override public void onInitializeAccessibilityNodeInfo(AccessibilityNodeInfo info){super.onInitializeAccessibilityNodeInfo(info);info.setClassName(android.widget.Button.class.getName());}
}
