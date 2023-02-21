# Lukla
Lukla is an API to create real world heightmaps based on [Shuttle Radar Topography Mission (SRTM30m)](https://en.wikipedia.org/wiki/Shuttle_Radar_Topography_Mission) digital elevation model. 

This program is pretty incomplete yet. I need to write unit tests and add some features.

![heigthmap](https://user-images.githubusercontent.com/7998054/216774590-7bf1eeb4-72a1-4731-8b60-4e09ed329f2d.png)

## Build instructions

First, you need SRTM30 digital elevation model data files in order to create heightmap tiles. 
This dataset is huge, so it is impossible to host those files in this repository. You need to download
the HGT files and put them in ./data/dem folder (you can change this directory using enviroment variables). You can download them using [this tool](https://dwtkns.com/srtm30m/).
Remember you need to create a NASA Earthdata account. You can register [here](https://urs.earthdata.nasa.gov/users/new), it's free.

### With Make

You can use Make to compile. Just use one of the following commands to compile to your target OS:

- ```make build-linux```
- ```make build-windows```
- ```make build-darwin``` (MacOS)

## Enviroment variables
None of the following variables are mandatory, but you will probably need some of them to correctly set-up the API.

* **LUKLA_ALLOWED_ORIGINS**: API allowed origins, separated by commas (,). If not defined, default is *http://localhost:PORT*;
* **LUKLA_PORT**: API HTTP port. Default is *9000*;
* **LUKLA_BASE_PATH**: API base path. Default is */*;
* **LUKLA_TILES_PATH**: Directory where generated heightmap images are cached. Default is *./data/tiles*;
* **DEM_FILES_PATH**: Directory where SRTM30 Digital elevation model .hgt files are stored. Default is *./data/dem*.

## Roadmap

This is a pretty simple project and it might be improved.

- Write unit tests and improve the code testability;
- Dockerize the app;
- Capability to create a heightmap based on a bounding box (instead of just use OSM tiles);
- Support to different zoom levels when creating OSM tiles (lower zoom levels must use bigger DEM resolutions in order to maintain a good perfomance). Now, Lukla just support zoom levels bigger than 10;
- Create a way to download SRTM30m files from NASA server;
- Support to different image extensions (e.g.: maybe TIFF), instead of just PNG files;
- An option to cache tiles in AWS S3 (or other cloud storages).
