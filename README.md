# Lukla
 Lukla is an API to create heightmaps based on SRTM30 digital elevation model. 

![heigtmap](https://user-images.githubusercontent.com/7998054/216774590-7bf1eeb4-72a1-4731-8b60-4e09ed329f2d.png)

## Build instructions

First, you need SRTM30 digital elevation model data files in order to create heightmap tiles. 
This dataset is huge, so it is impossible to host those files in this repository. So you need to download
the HGT files and in data/unzipped folder. You can download them using [this tool](https://dwtkns.com/srtm30m/).
Remember you need to create a NASA Earthdata account. You can register [here](https://urs.earthdata.nasa.gov/users/new), it's free.

### With Make

You can use Make to compile. Just use one of the following commands to compile to your target OS:

- ```make build-linux```
- ```make build-windows```
- ```make build-darwin``` (MacOS)

## Roadmap

This is a pretty simple project and it might be improved.

- Write unit tests and improve the code testability;
- Create a cache mecanism to store tiles and prevent them to be reacreated each request.
