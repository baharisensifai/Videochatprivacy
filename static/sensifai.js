var canvasElementWidth, canvasElementHeight;
var canvasCtx;
var videoSrc;
var filterDsc;

function removeBackground(results) {

  canvasCtx.clearRect(0, 0, canvasElementWidth, canvasElementHeight);
  canvasCtx.drawImage(
    results.segmentationMask,
    0,
    0,
    canvasElementWidth,
    canvasElementHeight
  );

  // Only overwrite existing pixels.
  canvasCtx.globalCompositeOperation = "source-out";
  canvasCtx.fillStyle = "#00FF00";
  canvasCtx.fillRect(0, 0, canvasElementWidth, canvasElementHeight);

  // Only overwrite missing pixels.
  canvasCtx.globalCompositeOperation = "destination-atop";
  canvasCtx.drawImage(
    results.image,
    0,
    0,
    canvasElementWidth,
    canvasElementHeight
  );
}    
  
function blurBackground(results) {
  canvasCtx.clearRect(0, 0, canvasElementWidth, canvasElementHeight);
  canvasCtx.drawImage(
    results.segmentationMask,
    0,
    0,
    canvasElementWidth,
    canvasElementHeight
  );

  canvasCtx.globalCompositeOperation = "source-out";
  canvasCtx.filter = 'blur(9px)';
  canvasCtx.drawImage(
    results.image,
    0,
    0,
    canvasElementWidth,
    canvasElementHeight
  );


  canvasCtx.globalCompositeOperation = "destination-atop";
  canvasCtx.filter = 'none';
  canvasCtx.drawImage(
    results.image,
    0,
    0,
    canvasElementWidth,
    canvasElementHeight
  );
}


function onResults(results) {
  switch(filterDsc) {
    case "Remove Background":
        removeBackground(results);
    break;
    case "Blur Background":
       blurBackground(results);
    break;
  }
}     
const pose = new Pose({
  locateFile: (file) => {
    return `/external/pose/${file}`;
  },
});

pose.setOptions({
  modelComplexity: 1,
  smoothLandmarks: true,
  enableSegmentation: true,
  smoothSegmentation: true,
  minDetectionConfidence: 0.5,
  minTrackingConfidence: 0.5,
});

pose.onResults(onResults);

async function sensifaiFilter() {
  
  try { 
     await pose.send({ image: videoSrc });
   }
   catch(e) {
     console.log("The AI model has not been downloaded, please wait... ");
  }        
}

document.addEventListener("DOMContentLoaded", function(event) {
    sensifaiFilter();
});