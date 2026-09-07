'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const measure = require('../static/js/measure.js');

const guides = { outer: { left: 10, right: 90, top: 10, bottom: 90 }, inner: { left: 14, right: 84, top: 16, bottom: 86 } };
test('centering uses card borders, independent of background and photo size', () => {
  const result = measure.measureBorders(guides);
  assert.equal(result.lr, 40);
  assert.equal(result.tb, 60);
  assert.equal(result.worst, 60);
  const shifted = Object.fromEntries(Object.entries(guides).map(([key, rect]) => [key, Object.fromEntries(Object.entries(rect).map(([edge, value]) => [edge, value * 0.5 + 20]))]));
  assert.deepEqual(measure.measureBorders(shifted), result);
});
test('invalid or zero width borders never produce an invented perfect grade', () => {
  assert.equal(measure.measureBorders({ outer: guides.outer, inner: guides.outer }), null);
  assert.equal(measure.measureBorders({ outer: guides.outer, inner: { ...guides.inner, left: 5 } }), null);
  assert.equal(measure.measureBorders({ outer: guides.outer, inner: { ...guides.inner, top: NaN } }), null);
});
test('moving any guide preserves the ordering of all eight markers', () => {
  const next = measure.moveGuide(guides, 'inner', 'left', 99);
  assert.ok(next.inner.left < next.inner.right);
  assert.ok(next.inner.right < next.outer.right);
  assert.equal(guides.inner.left, 14);
  const outside = measure.moveGuide(guides, 'outer', 'right', -100);
  assert.ok(outside.outer.right > outside.inner.right);
});
test('grade boundaries use unrounded values and the worse axis', () => {
  assert.equal(measure.gradeFor('PSA', { lr: 50, tb: 55 }, 'front').grade, '10');
  assert.equal(measure.gradeFor('PSA', { lr: 50, tb: 55.001 }, 'front').grade, '9');
  assert.equal(measure.gradeFor('PSA', { lr: 75, tb: 50 }, 'back').grade, '10');
  assert.equal(measure.gradeFor('PSA', null, 'front'), null);
  assert.equal(measure.gradeFor('BGS', { lr: 50, tb: 55 }, 'front').grade, '9.5');
  assert.equal(measure.gradeFor('BGS', { lr: 54, tb: 54 }, 'front').grade, '9');
});
test('projective mapping maps all four corners and rejects crossed corners', () => {
  const corners = [{ x: 10, y: 20 }, { x: 90, y: 10 }, { x: 80, y: 95 }, { x: 20, y: 80 }];
  const map = measure.perspectiveMap(corners);
  [[0,0],[1,0],[1,1],[0,1]].forEach(([u,v], i) => {
    const p = map(u,v);
    assert.ok(Math.abs(p.x - corners[i].x) < 1e-8);
    assert.ok(Math.abs(p.y - corners[i].y) < 1e-8);
  });
  assert.throws(() => measure.perspectiveMap([corners[0], corners[2], corners[1], corners[3]]), /corners/i);
});


test('suggested lines detect high contrast borders and refuse a featureless image', () => {
  const width=200,height=280,data=new Uint8ClampedArray(width*height*4);
  for(let y=0;y<height;y++)for(let x=0;x<width;x++){
    const card=x>=20&&x<180&&y>=20&&y<260,inner=x>=30&&x<164&&y>=35&&y<240;
    const value=inner?70:card?240:20,at=(y*width+x)*4;
    data.set([value,value,value,255],at);
  }
  const found=measure.detectGuides({width,height,data});
  assert.ok(found);
  assert.ok(Math.abs(found.outer.left-10)<1);
  assert.ok(Math.abs(found.inner.right-82)<1);
  assert.ok(Math.abs(measure.measureBorders(found).lr-100*10/26)<2);
  assert.equal(measure.detectGuides({width,height,data:new Uint8ClampedArray(data.length)}),null);
});

test('front and back keep independent photos and confirmation state', () => {
  const state=measure.createMeasure();
  state.sides.front.image='data:image/png;base64,fixture';
  state.sides.front.guides=structuredClone(guides);
  assert.equal(state.ready,false);
  state.confirm();assert.equal(state.ready,true);
  state.setSide('back');assert.equal(state.metrics,null);assert.equal(state.ready,false);
  state.setSide('front');assert.equal(state.metrics.lr,40);assert.equal(state.ready,true);
  state.resetGuides();assert.equal(state.ready,false);
  assert.ok(state.stageStyle.includes('display:block'));
  state.newCard();assert.equal(state.metrics,null);assert.ok(state.stageStyle.includes('display:none'));
});

test('zoom is bounded and returning to fit clears pan without changing measurements', () => {
  const state=measure.createMeasure();
  state.sides.front.guides=structuredClone(guides);
  state.setZoom(50);assert.equal(state.zoom,15);
  state.panX=50;state.panY=-30;state.setZoom(-1);
  assert.deepEqual([state.zoom,state.panX,state.panY],[1,0,0]);
  assert.deepEqual(state.sides.front.guides,guides);
});

test('missing back thresholds are not treated as perfect grades', () => {
  assert.equal(measure.gradeFor('SGC',{lr:50,tb:50},'back'),null);
  assert.equal(measure.gradeFor('TAG',{lr:65,tb:50},'back').grade,'10');
  assert.equal(measure.gradeFor('TAG',{lr:65.01,tb:50},'back').grade,'9');
  assert.equal(measure.gradeFor('ACE',{lr:59.99,tb:50},'front').grade,'10');
  assert.equal(measure.gradeFor('ACE',{lr:60,tb:50},'front').grade,'9');
});

test('perspective editing hides live guides and invalidates grade display', () => {
  const state=measure.createMeasure();state.sides.front.image='fixture';state.confirm();
  state.startPerspective();
  assert.equal(state.ready,false);
  assert.ok(state.lineStyle(state.guideList[0]).includes('display:none'));
  assert.equal(state.corners.length,4);
});

test('combined comparison requires both confirmed faces and both published thresholds', () => {
  assert.equal(measure.gradeForBoth('PSA',{lr:50,tb:50},{lr:80,tb:50}).grade,'9');
  assert.equal(measure.gradeForBoth('BGS',{lr:50,tb:55},{lr:65,tb:50}).grade,'9');
  assert.equal(measure.gradeForBoth('SGC',{lr:50,tb:50},{lr:50,tb:50}),null);
  const state=measure.createMeasure();state.gradeScope='both';
  state.sides.front.image='fixture';state.confirm();
  assert.equal(state.bothReady,false);
  assert.ok(state.grades.every(row=>row.display==='—'));
  state.setSide('back');state.sides.back.image='fixture';state.confirm();
  assert.equal(state.bothReady,true);
  assert.equal(state.grades.find(row=>row.company==='PSA').display,'10');
  state.startPerspective();assert.equal(state.bothReady,false);
});

test('cropping preserves asymmetric border ratios and resets the image boundary', () => {
  const cropped=measure.croppedGuides(guides);
  assert.deepEqual(cropped.outer,{left:0,right:100,top:0,bottom:100});
  const before=measure.measureBorders(guides),after=measure.measureBorders(cropped);
  assert.ok(Math.abs(before.lr-after.lr)<1e-10);
  assert.ok(Math.abs(before.tb-after.tb)<1e-10);
  assert.throws(()=>measure.croppedGuides({}),/eight guides/);
});

test('image import rejects unsupported and oversized files before decoding', async () => {
  await assert.rejects(measure.imageFromFile({type:'text/html',size:10}),/JPG, PNG or WebP/);
  await assert.rejects(measure.imageFromFile({type:'image/png',size:16*1024*1024}),/15 MB/);
  const state=measure.createMeasure();
  await state.loadFile({type:'text/html',size:10});
  assert.equal(state.busy,false);
  assert.equal(state.current.image,'');
  assert.match(state.error,/JPG, PNG or WebP/);
});

test('reviewed guides update the comparison live and cannot move during image processing', () => {
  const state=measure.createMeasure();state.current.image='fixture';state.confirm();
  const before=state.metrics.lr;state.nudge(0.1);
  assert.equal(state.ready,true);assert.notEqual(state.metrics.lr,before);
  state.busy=true;const position=state.selectedValue;state.nudge(1);
  assert.equal(state.selectedValue,position);
});

test('edits during an IndexedDB write remain visibly unsaved', async () => {
  const previous=global.indexedDB;
  let transaction, stored, started;
  const writeStarted=new Promise(resolve=>{started=resolve;});
  global.indexedDB={open(){
    const request={};
    queueMicrotask(()=>{
      request.result={close(){},transaction(){
        transaction={objectStore(){return {put(record){stored=record;return {};}};}};
        return transaction;
      }};
      request.onsuccess();started();
    });
    return request;
  }};
  try{
    const state=measure.createMeasure();state.current.image='fixture';state.title='First title';state.markChanged();state.refreshSaved=async()=>{};
    const saving=state.save();await writeStarted;
    state.title='Later title';state.markChanged();transaction.oncomplete();await saving;
    assert.equal(stored.title,'First title');assert.equal(state.title,'Later title');
    assert.equal(state.dirty,true);assert.equal(state.busy,false);assert.match(state.notice,/latest edits/);
  }finally{if(previous===undefined)delete global.indexedDB;else global.indexedDB=previous;}
});
